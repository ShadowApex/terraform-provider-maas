package main

import (
	"fmt"
	"log"
	"net/url"
	"time"

	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/juju/gomaasapi"
)

// resourceMAASMachineCreate Manages the commisioning of a new maas node
func resourceMAASMachineCreate(d *schema.ResourceData, meta interface{}) error {
	log.Println("[DEBUG] [resourceMAASMachineCreate] Launching new maas_node")

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
			nodeObj, err := getSingleNodeByMAC(meta.(*Config).MAASObject, macAddress)
			if err != nil {
				log.Printf("[ERROR] [resourceMAASMachineCreate] Unable to locate node by ID: %v.", err)
				return nil, "missing", nil
			}
			return nodeObj, "exists", nil
		},
		Timeout:    5 * time.Minute,
		Delay:      20 * time.Second,
		MinTimeout: 3 * time.Second,
	}
	if _, err := waitToExistConf.WaitForState(); err != nil {
		return fmt.Errorf("[ERROR] [resourceMAASMachineCreate] Error waiting for node with mac %s to exist: %s", macAddress, err)
	}

	nodeObj, err := getSingleNodeByMAC(meta.(*Config).MAASObject, macAddress)
	if err != nil {
		log.Println("[ERROR] [resourceMAASMachineCreate] Unable to locate node by ID.")
		return fmt.Errorf("No node with MAC address '%s' was found: %v", macAddress, err)
	}

	d.SetId(nodeObj.system_id)

	// update node
	params := url.Values{}
	if hostname, ok := d.GetOk("hostname"); ok {
		params.Add("hostname", hostname.(string))
	}

	if domain, ok := d.GetOk("domain"); ok {
		params.Add("domain", domain.(string))
	}

	const powerPrefix = "power.0"
	if _, ok := d.GetOk(powerPrefix); ok {
		if ptype, ok := d.GetOk(powerPrefix + ".type"); ok {
			params.Set("power_type", ptype.(string))
		}

		if user, ok := d.GetOk(powerPrefix + ".user"); ok {
			params.Set("power_parameters_power_user", user.(string))
		}

		if password, ok := d.GetOk(powerPrefix + ".password"); ok {
			params.Set("power_parameters_power_password", password.(string))
		}

		if address, ok := d.GetOk(powerPrefix + ".address"); ok {
			params.Set("power_parameters_power_address", address.(string))
		}

		if custom, ok := d.GetOk(powerPrefix + ".custom"); ok {
			values, ok := custom.(map[string]interface{})
			if !ok {
				return fmt.Errorf("Invalid type for power management custom values")
			}
			for k, v := range values {
				params.Set("power_parameters_"+k, v.(string))
			}
		}
	}

	err = nodeUpdate(meta.(*Config).MAASObject, d.Id(), params)
	if err != nil {
		log.Println("[DEBUG] Unable to update node")
		return fmt.Errorf("Failed to update node options (%v): %v", params, err)
	}

	// update node tags
	if tags, ok := d.GetOk("tags"); ok {
		for i := range tags.([]interface{}) {
			err := nodeTagsUpdate(meta.(*Config).MAASObject, d.Id(), tags.([]interface{})[i].(string))
			if err != nil {
				log.Printf("[ERROR] Unable to update node (%s) with tag (%s)", d.Id(), tags.([]interface{})[i].(string))
			}
		}
	}

	// commission the node
	if err := nodeDo(meta.(*Config).MAASObject, d.Id(), "commission", url.Values{}); err != nil {
		log.Printf("[ERROR] [resourceMAASMachineCreate] Unable to power up node: %s\n", d.Id())
		return err
	}

	log.Printf("[DEBUG] [resourceMAASMachineCreate] Waiting for commisioning (%s) to complete\n", d.Id())
	waitToCommissionConf := &resource.StateChangeConf{
		Pending:    []string{gomaasapi.NodeStatusCommissioning /*gomaasapi.NodeStatusTesting*/, "21"},
		Target:     []string{gomaasapi.NodeStatusReady},
		Refresh:    getNodeStatus(meta.(*Config).MAASObject, d.Id()),
		Timeout:    25 * time.Minute,
		Delay:      20 * time.Second,
		MinTimeout: 3 * time.Second,
	}

	if _, err := waitToCommissionConf.WaitForState(); err != nil {
		return fmt.Errorf("[ERROR] [resourceMAASMachineCreate] Error waiting for commisioning (%s) to complete: %s", d.Id(), err)
	}

	//
	if _, ok := d.GetOk("interface.0"); ok {
		ifaceMode := d.Get("interface.0.mode").(string)
		ifaceSubnet := d.Get("interface.0.subnet").(string)
		if interfaces, err := nodeGetInterfaces(meta.(*Config).MAASObject, d.Id()); err != nil {
			for _, item := range interfaces {
				// get the list of network interfaces
				iface, err := item.GetMAASObject()
				if err != nil {
					log.Printf("[ERROR] [resourceMAASMachineCreate] Unable to get interface ID")
					return fmt.Errorf("Failed to parse interfaces")
				}
				ifaceID, err := iface.GetField("id")
				if err != nil {
					log.Printf("[ERROR] [resourceMAASMachineCreate] Unable to get interface ID")
					return fmt.Errorf("Failed to parse interfaces")
				}
				ifaceParams := url.Values{
					"mode":   []string{ifaceMode},
					"subnet": []string{ifaceSubnet},
				}

				if err := interfaceDo(meta.(*Config).MAASObject, d.Id(), ifaceID, "link-subnet", ifaceParams); err != nil {
					log.Printf("[ERROR] [resourceMAASMachineCreate] Unable to setup interface: %s, %v\n", d.Id(), err)
				}
			}
		}
	}

	// setup the network interface
	err = nodeUpdate(meta.(*Config).MAASObject, d.Id(), params)
	if err != nil {
		log.Println("[DEBUG] Unable to update node")
		return fmt.Errorf("Failed to update node options (%v): %v", params, err)
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
