package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/hashicorp/terraform/helper/schema"
	"github.com/juju/gomaasapi"
)

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

	// Build a mapping of interface name to ID, and delete any virtual ifaces
	nameToIface := map[string]gomaasapi.Interface{}
	for _, iface := range machine.InterfaceSet() {
		log.Printf("[DEBUG] [updateMachineInterfaces] Found interface '%s' with id '%v'", iface.Name(), iface.ID())
		// Delete all virtual interface types. These will be re-created
		// if needed.
		switch iface.Type() {
		case "bond":
			log.Printf("[DEBUG] [updateMachineInterfaces] Deleting virtual interface '%s'", iface.Name())
			if err := iface.Delete(); err != nil {
				return err
			}
			continue
		}
		nameToIface[iface.Name()] = iface
	}

	// Loop through all defined interfaces
	interfaces := d.Get("interface").(*schema.Set)
	for _, item := range interfaces.List() {
		ifaceBlock := item.(map[string]interface{})
		name := ifaceBlock["name"].(string)
		subnetCIDR := ifaceBlock["subnet"].(string)
		mode := ifaceBlock["mode"].(string)
		bonds := ifaceBlock["bond"].(*schema.Set).List()

		// get the interface from MAAS
		if _, ok := nameToIface[name]; !ok {
			log.Printf("[DEBUG] [updateMachineInterfaces] Interface '%s' does not exist yet", name)

			// create any bonds if such parameters exist
			for _, b := range bonds {
				bondParams := b.(map[string]interface{})
				parents := []gomaasapi.Interface{}
				if parentsBlock, ok := bondParams["parents"]; ok {
					for _, parent := range parentsBlock.(*schema.Set).List() {
						parents = append(parents, nameToIface[parent.(string)])
					}
				}
				nameToIface[name], err = createBond(machine, name, parents, bondParams)
				if err != nil {
					return err
				}
			}
		}
		iface := nameToIface[name]

		// skip linking if no subnet is defined
		if subnetCIDR == "" {
			continue
		}

		// link the device to a subnet
		subnet, ok := cidrToSubnet[subnetCIDR]
		if !ok {
			return fmt.Errorf("No subnet CIDR %s exists", subnetCIDR)
		}

		// unlink first
		for _, link := range iface.Links() {
			if link.Subnet() == nil {
				continue
			}
			log.Printf("[DEBUG] Unlinking interface %s from subnet %s", name, link.Subnet().CIDR())
			err := iface.UnlinkSubnet(link.Subnet())
			if err != nil {
				return err
			}
		}

		// now link the correct subnet
		log.Printf("[DEBUG] [updateMachineInterfaces] Linking interface %s to subnet %s (mode: %s)", name, subnetCIDR, mode)
		args := gomaasapi.LinkSubnetArgs{
			Mode:   gomaasapi.InterfaceLinkMode(strings.ToUpper(mode)),
			Subnet: subnet,
		}
		err = iface.LinkSubnet(args)
		if err != nil {
			return err
		}
	}
	return nil
}

// creates a new bond device on the given machine
func createBond(machine gomaasapi.Machine, name string, parents []gomaasapi.Interface, bondParams map[string]interface{}) (gomaasapi.Interface, error) {
	args := gomaasapi.CreateMachineBondArgs{
		UpdateInterfaceArgs: gomaasapi.UpdateInterfaceArgs{
			Name:               name,
			BondMode:           bondParams["mode"].(string),
			MACAddress:         bondParams["mac_address"].(string),
			BondMiimon:         bondParams["miimon"].(int),
			BondDownDelay:      bondParams["downdelay"].(int),
			BondUpDelay:        bondParams["updelay"].(int),
			BondLACPRate:       bondParams["lacp_rate"].(string),
			BondXmitHashPolicy: bondParams["xmit_hash_policy"].(string),
		},
		Parents: parents,
	}

	log.Printf("[DEBUG] [createBond] Creating bond '%s' with parameters: %#v", name, args)
	bondIface, err := machine.CreateBond(args)
	if err != nil {
		return nil, fmt.Errorf("Failed to create bond: %v", err)
	}
	return bondIface, nil
}

func buildInterface(iface gomaasapi.Interface) map[string]interface{} {
	ifaceTf := map[string]interface{}{}
	ifaceTf["name"] = iface.Name()
	if len(iface.Links()) > 0 {
		ifaceTf["mode"] = iface.Links()[0].Mode()
		subnet := iface.Links()[0].Subnet()
		if subnet != nil {
			ifaceTf["subnet"] = iface.Links()[0].Subnet().CIDR()
		}
	}
	if iface.Type() == "bond" {
		bond := []map[string]interface{}{}
		bond = append(bond, buildBond(iface))
		ifaceTf["bond"] = bond
	}

	return ifaceTf
}

func buildBond(iface gomaasapi.Interface) map[string]interface{} {
	bond := map[string]interface{}{}
	bond["parents"] = iface.Parents()
	bond["mac_address"] = iface.MACAddress()
	params := iface.Params()
	bond["miimon"] = params.BondMiimon()
	bond["downdelay"] = params.BondDownDelay()
	bond["updelay"] = params.BondUpDelay()
	bond["lacp_rate"] = params.BondLACPRate()
	bond["xmit_hash_policy"] = params.BondXmitHashPolicy()
	bond["mode"] = params.BondMode()

	return bond
}
