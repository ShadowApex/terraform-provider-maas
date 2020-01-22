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

// resourceMAASNodeCreate Manages the commisioning of a new maas node
func resourceMAASNodeCreate(d *schema.ResourceData, meta interface{}) error {
	log.Println("[DEBUG] [resourceMAASNodeCreate] Launching new maas_node")

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
	log.Printf("[DEBUG] [resourceMAASNodeCreate] Waiting for node with mac %s to exist\n", macAddress)
	waitToExistConf := &resource.StateChangeConf{
		Pending: []string{"missing"},
		Target:  []string{"exists"},
		Refresh: func() (interface{}, string, error) {
			nodeObj, err := getSingleNodeByMAC(meta.(*Config).MAASObject, macAddress)
			if err != nil {
				log.Printf("[ERROR] [resourceMAASNodeCreate] Unable to locate node by ID: %v.", err)
				return nil, "missing", nil
			}
			return nodeObj, "exists", nil
		},
		Timeout:    1 * time.Minute,
		Delay:      10 * time.Second,
		MinTimeout: 3 * time.Second,
	}
	if _, err := waitToExistConf.WaitForState(); err != nil {
		return fmt.Errorf("[ERROR] [resourceMAASNodeCreate] Error waiting for node with mac %s to exist: %s", macAddress, err)
	}

	nodeObj, err := getSingleNodeByMAC(meta.(*Config).MAASObject, macAddress)
	if err != nil {
		log.Println("[ERROR] [resourceMAASNodeCreate] Unable to locate node by ID.")
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

	err = nodeUpdate(meta.(*Config).MAASObject, d.Id(), params)
	if err != nil {
		log.Println("[DEBUG] Unable to update node")
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

	if err := nodeDo(meta.(*Config).MAASObject, d.Id(), "commission", url.Values{}); err != nil {
		log.Printf("[ERROR] [resourceMAASNodeCreate] Unable to power up node: %s\n", d.Id())
		return err
	}

	log.Printf("[DEBUG] [resourceMAASNodeCreate] Waiting for commisioning (%s) to complete\n", d.Id())
	waitToCommissionConf := &resource.StateChangeConf{
		Pending:    []string{gomaasapi.NodeStatusCommissioning},
		Target:     []string{gomaasapi.NodeStatusReady},
		Refresh:    getNodeStatus(meta.(*Config).MAASObject, d.Id()),
		Timeout:    25 * time.Minute,
		Delay:      10 * time.Second,
		MinTimeout: 3 * time.Second,
	}

	if _, err := waitToCommissionConf.WaitForState(); err != nil {
		return fmt.Errorf("[ERROR] [resourceMAASNodeCreate] Error waiting for commisioning (%s) to complete: %s", d.Id(), err)
	}

	return resourceMAASNodeUpdate(d, meta)
}

// resourceMAASNodeRead read node information from a maas node
func resourceMAASNodeRead(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[DEBUG] Reading node (%s) information.\n", d.Id())
	return nil
}

// resourceMAASNodeUpdate update a node in terraform state
func resourceMAASNodeUpdate(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[DEBUG] [resourceMAASNodeUpdate] Modifying deployment %s\n", d.Id())

	d.Partial(true)

	d.Partial(false)

	log.Printf("[DEBUG] Done Modifying node %s", d.Id())
	return resourceMAASNodeRead(d, meta)
}

// resourceMAASDeploymentDelete will release the commisioning
func resourceMAASNodeDelete(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[DEBUG] Deleting node %s\n", d.Id())
	// remove tags
	if tags, ok := d.GetOk("tags"); ok {
		for i := range tags.([]interface{}) {
			err := nodeTagsRemove(meta.(*Config).MAASObject, d.Id(), tags.([]interface{})[i].(string))
			if err != nil {
				log.Printf("[ERROR] Unable to update node (%s) with tag (%s)", d.Id(), tags.([]interface{})[i].(string))
			}
		}
	}

	log.Printf("[DEBUG] [resourceMAASDeploymentDelete] Node (%s) decomissioned", d.Id())

	d.SetId("")
	return nil
}
