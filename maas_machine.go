package main

import (
	"fmt"
	"log"
	"time"

	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/juju/gomaasapi"
)

func makeMachineArgs(d *schema.ResourceData) gomaasapi.UpdateMachineArgs {
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
			cidrToSubnet[subnet.CIDR()] = subnet
		}
	}

	// Build a mapping of interface name to ID
	nameToIface := map[string]gomaasapi.Interface{}
	for _, iface := range machine.InterfaceSet() {
		nameToIface[iface.Name()] = iface
	}

	for i := 0; i < d.Get("interface.#").(int); i++ {
		curParams := d.Get(fmt.Sprintf("interface.%d", i)).(*schema.ResourceData)
		name := curParams.Get("name").(string)
		log.Printf("[DEBUG] [resourceMAASMachineCreate] Updating interface %s", name)
		if bondBlock, ok := curParams.GetOk("bond.0"); ok {
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

			if bondIface, err := machine.CreateBond(args); err != nil {
				return fmt.Errorf("Failed to create bond: %v", err)
			} else {
				nameToIface[name] = bondIface
			}
		}

		// link the device to a subnet
		log.Printf("[DEBUG] [resourceMAASMachineCreate] Linking interface %s to subnet", name)
		subnetCIDR := curParams.Get("subnet").(string)
		subnet, ok := cidrToSubnet[subnetCIDR]
		if !ok {
			return fmt.Errorf("No subnet CIDR %s exists: %s", subnetCIDR)
		}
		args := gomaasapi.LinkSubnetArgs{
			Mode:   gomaasapi.InterfaceLinkMode(curParams.Get("mode").(string)),
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

	// wait for the node to exist, if it was just created as another terraform resource
	// it might take a few minutes to PXE boot and show up in MaaS
	log.Printf("[DEBUG] [resourceMAASMachineCreate] Waiting for node with mac %s to exist\n", macAddress)
	waitToExistConf := &resource.StateChangeConf{
		Pending: []string{"missing"},
		Target:  []string{"exists"},
		Refresh: func() (interface{}, string, error) {
			log.Printf("[ERROR] [resourceMAASMachineCreate] Polling for machine with mac %s.", macAddress)
			nodes, err := controller.Machines(gomaasapi.MachinesArgs{MACAddresses: []string{macAddress}})
			if err != nil {
				log.Printf("[ERROR] [resourceMAASMachineCreate] Unable to locate node by mac: %v.", err)
				return nil, "", err
			}
			if len(nodes) == 0 {
				log.Printf("[DEBUG] [resourceMAASMachineCreate] no nodes with mac: %v.", macAddress)
				return nil, "missing", nil
			}
			return nodes[0], "exists", nil
		},
		Timeout:    5 * time.Minute,
		Delay:      20 * time.Second,
		MinTimeout: 3 * time.Second,
	}
	if _, err := waitToExistConf.WaitForState(); err != nil {
		return fmt.Errorf("[ERROR] [resourceMAASMachineCreate] Error waiting for node with mac %s to exist: %s", macAddress, err)
	}

	nodes, err := controller.Machines(gomaasapi.MachinesArgs{MACAddresses: []string{macAddress}})
	if err != nil || len(nodes) == 0 {
		log.Printf("[ERROR] [resourceMAASMachineCreate] Unable to locate node by mac: %v.", err)
		return err
	}
	machine := nodes[0]

	d.SetId(machine.SystemID())

	// update base machine options
	machineArgs := makeMachineArgs(d)
	err = machine.Update(machineArgs)
	if err != nil {
		log.Println("[DEBUG] Unable to update node")
		return fmt.Errorf("Failed to update node options: %v", err)
	}

	// add tags
	if tags, ok := d.GetOk("tags"); ok {
		for i := range tags.([]interface{}) {
			err := nodeTagsUpdate(meta.(*Config).MAASObject, d.Id(), tags.([]interface{})[i].(string))
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
		CommissioningScripts: d.Get("commissioning_scripts").([]string),
		TestingScripts:       d.Get("testing_scripts").([]string),
	}
	if err := machine.Commission(commissionArgs); err != nil {
		log.Printf("[ERROR] [resourceMAASMachineCreate] Unable to commission: %s\n", d.Id())
		return err
	}

	log.Printf("[DEBUG] [resourceMAASMachineCreate] Waiting for commisioning (%s) to complete\n", d.Id())
	waitToCommissionConf := &resource.StateChangeConf{
		Pending:    []string{gomaasapi.NodeStatusCommissioning, gomaasapi.NodeStatusTesting},
		Target:     []string{gomaasapi.NodeStatusReady},
		Refresh:    getNodeStatus(meta.(*Config).MAASObject, d.Id()),
		Timeout:    25 * time.Minute,
		Delay:      20 * time.Second,
		MinTimeout: 3 * time.Second,
	}

	if _, err := waitToCommissionConf.WaitForState(); err != nil {
		return fmt.Errorf("Failed waiting for commisioning (%s) to complete: %s", d.Id(), err)
	}

	err = updateMachineInterfaces(d, controller, machine)
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
	log.Printf("[DEBUG] [resourceMAASMachineUpdate] Modifying deployment %s\n", d.Id())

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
