package main

import (
	"fmt"
	"log"
	"time"

	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/juju/gomaasapi"
)

func makeCreateMachineArgs(d *schema.ResourceData) gomaasapi.CreateMachineArgs {
	args := gomaasapi.CreateMachineArgs{
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

		if user, ok := d.GetOk(powerPrefix + ".user"); ok {
			args.PowerUser = user.(string)
		}

		if password, ok := d.GetOk(powerPrefix + ".password"); ok {
			args.PowerPassword = password.(string)
		}

		if address, ok := d.GetOk(powerPrefix + ".address"); ok {
			args.PowerAddress = address.(string)
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

func updateMachineInterfaces(d *schema.ResourceData, controller gomaasapi.Controller, machine gomaasapi.Machine) error {
	// enumerate the subnets available
	cidrToSubnet := map[string]gomaasapi.Subnet{}
	spaces, err := controller.Spaces()
	if err != nil {
		return err
	}
	for _, space := range spaces {
		// Note: this will collapse subnets that overlap in different spaces
		// TODO: link up spaces better
		for _, subnet := range space.Subnets() {
			log.Printf("[DEBUG] Found CIDR %s in Space %s", space.Name(), subnet.CIDR())
			cidrToSubnet[subnet.CIDR()] = subnet
		}
	}

	// Build a mapping of interface name to ID
	nameToIface := map[string]gomaasapi.Interface{}
	for _, iface := range machine.InterfaceSet() {
		nameToIface[iface.Name()] = iface
	}

	for i := 0; i < d.Get("interface.#").(int); i++ {
		name := d.Get(fmt.Sprintf("interface.%d.name", i)).(string)
		log.Printf("[DEBUG] [resourceMAASMachineCreate] Updating interface %s", name)
		if bondBlock, ok := d.GetOk(fmt.Sprintf("interface.%d.name.bond.0", i)); ok {
			bondParams := bondBlock.(*schema.ResourceData)
			log.Printf("[DEBUG] [resourceMAASMachineCreate] Creating bond %s", name)
			// create a new bond device
			args := gomaasapi.CreateMachineBondArgs{
				UpdateInterfaceArgs: gomaasapi.UpdateInterfaceArgs{
					BondMode:           bondParams.Get("mode").(string),
					MACAddress:         bondParams.Get("mac_address").(string),
					BondMiimon:         bondParams.Get("miimon").(int),
					BondDownDelay:      bondParams.Get("downdelay").(int),
					BondUpDelay:        bondParams.Get("updelay").(int),
					BondLACPRate:       bondParams.Get("lacp_rate").(string),
					BondXmitHashPolicy: bondParams.Get("xmit_hash_policy").(string),
				},
				Parents: []gomaasapi.Interface{},
			}

			if parents, ok := bondParams.GetOk("parents"); ok {
				for _, parent := range parents.([]interface{}) {
					args.Parents = append(args.Parents, parent.(gomaasapi.Interface))
				}
			}

			bondIface, err := machine.CreateBond(args)
			if err != nil {
				return fmt.Errorf("Failed to create bond: %v", err)
			}
			nameToIface[name] = bondIface
		}

		// link the device to a subnet
		subnetCIDR := d.Get(fmt.Sprintf("interface.%d.subnet", i)).(string)
		subnet, ok := cidrToSubnet[subnetCIDR]
		if !ok {
			return fmt.Errorf("No subnet CIDR %s exists", subnetCIDR)
		}
		mode := d.Get(fmt.Sprintf("interface.%d.mode", i)).(string)
		log.Printf("[DEBUG] [resourceMAASMachineCreate] Linking interface %s to subnet %s (mode: %s)", name, subnetCIDR, mode)
		args := gomaasapi.LinkSubnetArgs{
			Mode:   gomaasapi.InterfaceLinkMode(mode),
			Subnet: subnet,
		}
		if iface, ok := nameToIface[name]; ok {
			err := iface.LinkSubnet(args)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// resourceMAASMachineCreate Manages the commisioning of a new maas node
func resourceMAASMachineCreate(d *schema.ResourceData, meta interface{}) error {
	log.Println("[DEBUG] [resourceMAASMachineCreate] Launching new maas_node")

	controller := meta.(*Config).Controller

	macAddressVal, set := d.GetOk("mac_address")
	if !set {
		return fmt.Errorf("Missing mac_address value")
	}
	macAddress, ok := macAddressVal.(string)
	if !ok {
		return fmt.Errorf("Invalid type for mac_address field")
	}

	// Attempt to create a new device (it might already exist)

	createArgs := makeCreateMachineArgs(d)
	_, err := controller.CreateMachine(createArgs)
	if err != nil {
		// is error "already exists?"
		log.Printf("[ERROR] [resourceMAASMachineCreate] Creating a device with mac: %s failed, it might already exist: %v.", macAddress, err)
	}

	// Locate the machine we either just created or was already auto-created
	machines, err := controller.Machines(gomaasapi.MachinesArgs{MACAddresses: []string{macAddress}})
	if err != nil {
		log.Printf("[ERROR] [resourceMAASMachineCreate] Unable to seach machines by mac: %v.", err)
		return err
	}
	if len(machines) == 0 {
		log.Printf("[DEBUG] [resourceMAASMachineCreate] no nodes with mac: %v.", macAddress)
		return fmt.Errorf("Failed to create or locate machine with mac %s", macAddress)
	}
	machine := machines[0]

	d.SetId(machine.SystemID())

	// update base machine options
	machineArgs := makeUpdateMachineArgs(d)
	err = machine.Update(machineArgs)
	if err != nil {
		log.Println("[DEBUG] Unable to update node")
		return fmt.Errorf("Failed to update node options: %v", err)
	}

	// add tags
	if tags, ok := d.GetOk("tags"); ok {
		for i := range tags.([]interface{}) {
			err := machineUpdateTags(meta.(*Config).Controller, machine, tags.([]interface{})[i].(string))
			if err != nil {
				log.Printf("[ERROR] Unable to update node (%s) with tag (%s)", d.Id(), tags.([]interface{})[i].(string))
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
		return err
	}

	log.Printf("[DEBUG] [resourceMAASMachineCreate] Waiting for commisioning (%s) to complete\n", d.Id())
	waitToCommissionConf := &resource.StateChangeConf{
		Pending:    []string{"Commissioning", "Testing"},
		Target:     []string{"Ready"},
		Refresh:    getMachineStatus(meta.(*Config).Controller, machine.SystemID()),
		Timeout:    25 * time.Minute,
		Delay:      20 * time.Second,
		MinTimeout: 3 * time.Second,
	}

	commissionedMachine, err := waitToCommissionConf.WaitForState()
	if err != nil {
		return fmt.Errorf("Failed waiting for commisioning (%s) to complete: %s", d.Id(), err)
	}

	err = updateMachineInterfaces(d, controller, commissionedMachine.(gomaasapi.Machine))
	if err != nil {
		return err
	}

	return resourceMAASMachineUpdate(d, meta)
}

// resourceMAASMachineRead read node information from a maas node
func resourceMAASMachineRead(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[DEBUG] Reading node (%s) information.\n", d.Id())
	return nil
}

// resourceMAASMachineUpdate update a node in terraform state
func resourceMAASMachineUpdate(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[DEBUG] [resourceMAASMachineUpdate] Modifying machine %s\n", d.Id())

	d.Partial(true)

	d.Partial(false)

	log.Printf("[DEBUG] Done Modifying node %s", d.Id())
	return resourceMAASMachineRead(d, meta)
}

// resourceMAASDeploymentDelete will release the commisioning
func resourceMAASMachineDelete(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[DEBUG] Deleting node %s\n", d.Id())
	err := nodeDelete(meta.(*Config).MAASObject, d.Id())
	if err != nil {
		log.Printf("[ERROR] Unable to delete node (%s): %v", d.Id(), err)
	}
	log.Printf("[DEBUG] [resourceMAASMachineDelete] Node (%s) deleted", d.Id())
	d.SetId("")
	return nil
}
