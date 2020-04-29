package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/juju/gomaasapi"
)

func makeCreateMachineArgs(d *schema.ResourceData) gomaasapi.CreateMachineArgs {
	args := gomaasapi.CreateMachineArgs{
		Commission:   false, // we manage the commision state
		MACAddresses: []string{},
	}
	args.UpdateMachineArgs = makeUpdateMachineArgs(d)

	if architecture, ok := d.GetOk("architecture"); ok {
		args.Architecture = architecture.(string)
	}

	if description, ok := d.GetOk("description"); ok {
		args.Description = description.(string)
	}

	if macAddress, ok := d.GetOk("mac_address"); ok {
		args.MACAddresses = []string{macAddress.(string)}
	}

	return args
}

func makeUpdateMachineArgs(d *schema.ResourceData) gomaasapi.UpdateMachineArgs {
	args := gomaasapi.UpdateMachineArgs{
		PowerOpts: map[string]string{},
	}

	if hostname, ok := d.GetOk("hostname"); ok {
		args.Hostname = hostname.(string)
	}

	if domain, ok := d.GetOk("domain"); ok {
		args.Domain = domain.(string)
	}

	const powerPrefix = "power.0"
	if _, ok := d.GetOk(powerPrefix); ok {
		if ptype, ok := d.GetOk(powerPrefix + ".type"); ok {
			args.PowerType = ptype.(string)
		}

		if custom, ok := d.GetOk(powerPrefix + ".custom"); ok {
			values := custom.(map[string]interface{})
			for k, v := range values {
				args.PowerOpts[k] = v.(string)
			}
		}
	}
	return args
}

// resourceMAASMachineCreate Manages the commisioning of a new maas node
func resourceMAASMachineCreate(d *schema.ResourceData, meta interface{}) error {
	log.Println("[DEBUG] [resourceMAASMachineCreate] Launching new machine")

	controller := meta.(*Config).Controller

	// Attempt to create a new device (it might already exist)

	createArgs := makeCreateMachineArgs(d)
	_, err := controller.CreateMachine(createArgs)
	if err != nil {
		// is error "already exists?"
		log.Printf("[ERROR] [resourceMAASMachineCreate] Creating a device failed, it might already exist: %v.", err)
	}

	macAddressVal, set := d.GetOk("mac_address")
	if !set {
		return fmt.Errorf("Missing mac_address value")
	}
	macAddress, ok := macAddressVal.(string)
	if !ok {
		return fmt.Errorf("Invalid type for mac_address field")
	}

	if macAddress == "" {
		return fmt.Errorf("Empty mac_address value")
	}

	// Locate the machine we either just created or was already auto-created
	machines, err := controller.Machines(gomaasapi.MachinesArgs{MACAddresses: []string{macAddress}})
	if err != nil {
		log.Printf("[ERROR] [resourceMAASMachineCreate] Unable to seach machines by mac: %v.", err)
		return err
	}
	if len(machines) == 0 {
		log.Printf("[DEBUG] [resourceMAASMachineCreate] no machine with mac: %v.", macAddress)
		return fmt.Errorf("Failed to create or locate machine with mac %s", macAddress)
	}
	machine := machines[0]

	d.SetId(machine.SystemID())

	// update base machine options
	machineArgs := makeUpdateMachineArgs(d)
	err = machine.Update(machineArgs)
	if err != nil {
		log.Println("[DEBUG] Unable to update machine")
		return fmt.Errorf("Failed to update machine options: %v", err)
	}

	// add tags
	if tags, ok := d.GetOk("tags"); ok {
		for _, item := range tags.(*schema.Set).List() {
			err := machineUpdateTags(meta.(*Config).Controller, machine, item.(string))
			if err != nil {
				log.Printf("[ERROR] Unable to update machine (%s) with tag (%s)", d.Id(), item.(string))
			}
		}
	}

	commissionArgs := gomaasapi.CommissionArgs{
		EnableSSH:            d.Get("enable_ssh").(bool),
		SkipBMCConfig:        d.Get("skip_bmc_config").(bool),
		SkipNetworking:       d.Get("skip_networking").(bool),
		SkipStorage:          d.Get("skip_storage").(bool),
		CommissioningScripts: []string{},
		TestingScripts:       []string{},
	}

	if scripts, ok := d.GetOk("commissioning_scripts"); ok {
		commissionArgs.CommissioningScripts = scripts.([]string)
	}
	if scripts, ok := d.GetOk("testing_scripts"); ok {
		commissionArgs.TestingScripts = scripts.([]string)
	}

	if err := machine.Commission(commissionArgs); err != nil {
		log.Printf("[ERROR] [resourceMAASMachineCreate] Unable to commission: %s\n", d.Id())
		_, stateName, _ := getMachineStatus(controller, machine.SystemID())()
		if stateName != "Commissioning" {
			// we were in a real unexpected state - bail
			log.Printf("[ERROR] [resourceMAASMachineCreate] commision request machine state: '%s'\n", stateName)
			return err
		}
		// ignore this error, we may have auto-entered commissioning state, not great but ok :|
	}

	log.Printf("[DEBUG] [resourceMAASMachineCreate] Waiting for commisioning (%s) to complete\n", d.Id())
	waitToCommissionConf := &resource.StateChangeConf{
		Pending:    []string{"Commissioning", "Testing"},
		Target:     []string{"Ready"},
		Refresh:    getMachineStatus(meta.(*Config).Controller, machine.SystemID()),
		Timeout:    25 * time.Minute,
		Delay:      60 * time.Second,
		MinTimeout: 30 * time.Second,
	}

	commissionedMachine, err := waitToCommissionConf.WaitForState()
	if err != nil {
		return fmt.Errorf("Failed waiting for commissioning (%s) to complete: %s", d.Id(), err)
	}

	err = updateMachineInterfaces(d, controller, commissionedMachine.(gomaasapi.Machine))
	if err != nil {
		return err
	}

	// release the machine so it can be deployed by another user
	err = controller.ReleaseMachines(gomaasapi.ReleaseMachinesArgs{SystemIDs: []string{machine.SystemID()}})
	if err != nil {
		return err
	}
	log.Printf("[DEBUG] [resourceMAASMachineCreate] Waiting for release (%s) to complete\n", d.Id())
	releaseConf := &resource.StateChangeConf{
		Pending:    []string{"Releasing"},
		Target:     []string{"Ready"},
		Refresh:    getMachineStatus(meta.(*Config).Controller, machine.SystemID()),
		Timeout:    5 * time.Minute,
		Delay:      60 * time.Second,
		MinTimeout: 30 * time.Second,
	}

	_, err = releaseConf.WaitForState()
	if err != nil {
		return fmt.Errorf("Failed waiting for release (%s) to complete: %s", d.Id(), err)
	}

	return resourceMAASMachineRead(d, meta)
}

// resourceMAASMachineRead read node information from a maas node
func resourceMAASMachineRead(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[DEBUG] Reading machine (%s) information.\n", d.Id())

	controller := meta.(*Config).Controller
	machine, err := controller.GetMachine(d.Id())
	if err != nil {
		return err
	}

	// Read the machine details
	d.Set("architecture", machine.Architecture())
	d.Set("hostname", machine.Hostname())
	d.Set("domain", strings.SplitN(machine.FQDN(), ".", 2)[1])

	// Read the boot interface MAC address
	ifaceBoot := machine.BootInterface()
	if ifaceBoot != nil {
		d.Set("mac_address", ifaceBoot.MACAddress())
	}

	// Read the interfaces
	interfaces := []map[string]interface{}{}
	for _, iface := range machine.InterfaceSet() {
		log.Printf("[DEBUG] Found interface with type: %s", iface.Type())
		// If the given interface has children, then we don't need to create it.
		if len(iface.Children()) > 0 {
			continue
		}
		ifaceTf := buildInterface(iface)
		interfaces = append(interfaces, ifaceTf)
	}
	if err := d.Set("interface", interfaces); err != nil {
		return err
	}

	// Read the block devices
	blockDevices := []map[string]interface{}{}
	for _, device := range machine.PhysicalBlockDevices() {
		log.Printf("[DEBUG] Found block device '%s'", device.Name())
		deviceTf := buildBlockDevice(device)
		blockDevices = append(blockDevices, deviceTf)
	}
	if err := d.Set("block_device", blockDevices); err != nil {
		return err
	}

	// Read the volume groups
	vgs, err := machine.VolumeGroups()
	if err != nil {
		log.Printf("[WARN] Error getting volume groups: %v", err)
		vgs = []gomaasapi.VolumeGroup{}
	}
	volumeGroups := []map[string]interface{}{}
	for _, vg := range vgs {
		log.Printf("[DEBUG] Found volume group: %v", vg)
		volumeGroups = append(volumeGroups, buildVolumeGroup(machine, vg))
	}
	d.Set("volume_group", volumeGroups)

	// Read the tags
	d.Set("tags", machine.Tags())

	// Handle power configuration
	pwr, err := controller.GetMachinePower(d.Id())
	if err != nil {
		return err
	}
	d.Set("power", []map[string]interface{}{
		buildPowerParams(machine, pwr),
	})

	log.Printf("[DEBUG] Done reading machine %s", d.Id())

	return nil
}

func buildPowerParams(machine gomaasapi.Machine, powerCfg map[string]string) map[string]interface{} {
	powerCfgTf := map[string]interface{}{}
	powerCfgTf["type"] = machine.PowerType()
	powerCfgTf["custom"] = powerCfg

	return powerCfgTf
}

// resourceMAASMachineUpdate update a node in terraform state
func resourceMAASMachineUpdate(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[DEBUG] [resourceMAASMachineUpdate] Modifying machine %s\n", d.Id())

	controller := meta.(*Config).Controller
	machine, err := controller.GetMachine(d.Id())
	if err != nil {
		return err
	}

	d.Partial(true)
	updateArgs := gomaasapi.UpdateMachineArgs{}
	needsUpdate := false
	if d.HasChange("hostname") {
		updateArgs.Hostname = d.Get("hostname").(string)
		needsUpdate = true
	}
	if d.HasChange("domain") {
		updateArgs.Domain = d.Get("domain").(string)
		needsUpdate = true
	}
	if d.HasChange("power") {
		updateMachinePower(d, &updateArgs)
		needsUpdate = true
	}
	if needsUpdate {
		err := machine.Update(updateArgs)
		if err != nil {
			return err
		}
	}

	if d.HasChange("block_device") {
		err = updateMachineBlockDevices(d, controller, machine)
		if err != nil {
			return err
		}
	}

	if d.HasChange("interface") {
		log.Printf("[DEBUG] Detected change to interface!")
		err = updateMachineInterfaces(d, controller, machine)
		if err != nil {
			return err
		}
	}

	if d.HasChange("tags") {
		hasTags := map[string]gomaasapi.Tag{}
		for _, t := range machine.Tags() {
			tag, err := controller.GetTag(t)
			if err != nil {
				return err
			}
			hasTags[t] = tag
		}
		wantTags := map[string]struct{}{}
		for _, t := range d.Get("tags").(*schema.Set).List() {
			wantTags[t.(string)] = struct{}{}
		}
		// add any missing tags
		for wantTag := range wantTags {
			_, has := hasTags[wantTag]
			if !has {
				var maasTag gomaasapi.Tag
				maasTag, err = controller.GetTag(wantTag)
				if err != nil {
					log.Printf("[DEBUG] Creating new MaaS tag %s", wantTag)
					maasTag, err = controller.CreateTag(gomaasapi.CreateTagArgs{Name: wantTag})
					if err != nil {
						return fmt.Errorf("Failed to get or create tag %s: %v", wantTag, err)
					}
				}
				log.Printf("[DEBUG] Adding tag %s to %s", maasTag.Name(), machine.Hostname())
				err := maasTag.AddToMachine(machine.SystemID())
				if err != nil {
					return fmt.Errorf("Failed to add tag %s to %s", wantTag, machine.Hostname())
				}
			}
		}
		// remove any extra tags
		for name, hasTag := range hasTags {
			_, doesWant := wantTags[name]
			if !doesWant {
				log.Printf("[DEBUG] Removing extra tag %s from %s", name, machine.Hostname())
				hasTag.RemoveFromMachine(machine.SystemID())
			}

		}
	}

	// TODO: power
	d.Partial(false)

	log.Printf("[DEBUG] Done Modifying machine %s", d.Id())
	return nil
}

func updateMachinePower(d *schema.ResourceData, updateArgs *gomaasapi.UpdateMachineArgs) {
	updateArgs.PowerType = "manual"
	powerDef := d.Get("power").(*schema.Set)
	for _, item := range powerDef.List() {
		power := item.(map[string]interface{})
		powerOpts := map[string]string{}
		updateArgs.PowerType = power["type"].(string)
		for key, value := range power["custom"].(map[string]interface{}) {
			powerOpts[key] = value.(string)
		}
		updateArgs.PowerOpts = powerOpts
	}
}

// resourceMAASDeploymentDelete will release the commisioning
func resourceMAASMachineDelete(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[DEBUG] Deleting node %s\n", d.Id())
	controller := meta.(*Config).Controller
	machines, err := controller.Machines(gomaasapi.MachinesArgs{SystemIDs: []string{d.Id()}})
	if err != nil {
		log.Printf("[ERROR] Unable to delete machine (%s): %v", d.Id(), err)
	}
	if len(machines) == 0 {
		return fmt.Errorf("Machine with id %s does not exist", d.Id())
	}
	err = machines[0].Delete()
	log.Printf("[DEBUG] [resourceMAASMachineDelete] machine (%s) deleted", d.Id())
	d.SetId("")
	return nil
}
