package main

import (
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/juju/gomaasapi"
)

func makeAllocateArgs(d *schema.ResourceData) (*gomaasapi.AllocateMachineArgs, error) {
	log.Println("[DEBUG] [makeAllocateArgs] Parsing any existing MAAS constraints")
	args := &gomaasapi.AllocateMachineArgs{}

	hostname, set := d.GetOk("hostname")
	if set {
		log.Printf("[DEBUG] [parseConstraints] setting hostname to %+v", hostname)
		args.Hostname = hostname.(string)
	}

	architecture, set := d.GetOk("architecture")
	if set {
		log.Printf("[DEBUG] [parseConstraints] Setting architecture to %s", architecture)
		args.Architecture = architecture.(string)
	}

	cpuCount, set := d.GetOk("cpu_count")
	if set {
		val, err := strconv.ParseInt(cpuCount.(string), 10, 64)
		if err != nil {
			return nil, err
		}
		args.MinCPUCount = int(val)
	}

	memory, set := d.GetOk("memory")
	if set {
		val, err := strconv.ParseInt(memory.(string), 10, 64)
		if err != nil {
			return nil, err
		}
		args.MinMemory = int(val)
	}

	tags, set := d.GetOk("tags")
	if set {
		args.Tags = make([]string, len(tags.([]interface{})))
		for i := range tags.([]interface{}) {
			args.Tags[i] = tags.([]interface{})[i].(string)
		}
	}

	return args, nil
}

func makeStartArgs(d *schema.ResourceData) gomaasapi.StartArgs {
	args := gomaasapi.StartArgs{}

	// get user data if defined
	if user_data, ok := d.GetOk("user_data"); ok {
		args.UserData = base64encode(user_data.(string))
	}

	// get comment if defined
	if comment, ok := d.GetOk("comment"); ok {
		args.Comment = comment.(string)
	}

	// get distro_series if defined
	distro_series, ok := d.GetOk("distro_series")
	if ok {
		args.DistroSeries = distro_series.(string)
	}

	return args
}

// resourceMAASDeploymentCreate This function doesn't really *create* a new node but, power an already registered
func resourceMAASDeploymentCreate(d *schema.ResourceData, meta interface{}) error {
	log.Println("[DEBUG] [resourceMAASDeploymentCreate] Launching new maas_deployment")

	/*
		According to the MAAS API documentation here: https://maas.ubuntu.com/docs/api.html
		We need to acquire or allocate a node before we start it.  We pass (as url.Values)
		some parameters that could be used to narrow down our selection (cpu_count, memory, etc.)
	*/

	controller := meta.(*Config).Controller

	allocateArgs, err := makeAllocateArgs(d)
	if err != nil {
		log.Println("[ERROR] [resourceMAASDeploymentCreate] Unable to parse constraints.")
		return err
	}
	machine, _, err := controller.AllocateMachine(*allocateArgs)
	if err != nil {
		log.Println("[ERROR] [resourceMAASDeploymentCreate] Unable to allocate machine")
		return err
	}

	// set the node id
	d.SetId(machine.SystemID())

	startArgs := makeStartArgs(d)
	if err = machine.Start(startArgs); err != nil {
		log.Printf("[ERROR] [resourceMAASDeploymentCreate] Unable to power up node: %s\n", d.Id())
		controller.ReleaseMachines(gomaasapi.ReleaseMachinesArgs{SystemIDs: []string{machine.SystemID()}})
		return err
	}

	log.Printf("[DEBUG] [resourceMAASDeploymentCreate] Waiting for deployment (%s) to become active\n", d.Id())
	stateConf := &resource.StateChangeConf{
		Pending:    []string{"Deploying"},
		Target:     []string{"Deployed"},
		Refresh:    getMachineStatus(meta.(*Config).Controller, d.Id()),
		Timeout:    25 * time.Minute,
		Delay:      10 * time.Second,
		MinTimeout: 3 * time.Second,
	}

	if _, err := stateConf.WaitForState(); err != nil {
		if err := controller.ReleaseMachines(gomaasapi.ReleaseMachinesArgs{SystemIDs: []string{machine.SystemID()}}); err != nil {
			log.Printf("[DEBUG] Unable to release node")
		}
		return fmt.Errorf("[ERROR] [resourceMAASDeploymentCreate] Error waiting for deployment (%s) to become deployed: %s", d.Id(), err)
	}

	updateArgs := gomaasapi.UpdateMachineArgs{}
	// update hostname
	if hostname, ok := d.GetOk("deploy_hostname"); ok {
		updateArgs.Hostname = hostname.(string)
	}
	err = machine.Update(updateArgs)
	if err != nil {
		log.Println("[DEBUG] Unable to update node")
		return err
	}

	// update node tags
	if tags, ok := d.GetOk("deploy_tags"); ok {
		for i := range tags.([]interface{}) {
			err := machineUpdateTags(controller, machine, tags.([]interface{})[i].(string))
			if err != nil {
				log.Printf("[ERROR] Unable to update node (%s) with tag (%s)", d.Id(), tags.([]interface{})[i].(string))
			}
		}
	}

	return resourceMAASDeploymentUpdate(d, meta)
}

// resourceMAASDeploymentRead read deployment information from a maas node
// TODO: remove or do something
func resourceMAASDeploymentRead(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[DEBUG] Reading deployment (%s) information.\n", d.Id())
	return nil
}

// resourceMAASDeploymentUpdate update an deployment in terraform state
func resourceMAASDeploymentUpdate(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[DEBUG] [resourceMAASDeploymentUpdate] Modifying deployment %s\n", d.Id())

	d.Partial(true)

	d.Partial(false)

	log.Printf("[DEBUG] Done Modifying deployment %s", d.Id())
	return resourceMAASDeploymentRead(d, meta)
}

// resourceMAASDeploymentDelete This function doesn't really *delete* a maas managed deployment but releases (read, turns off) the node.
func resourceMAASDeploymentDelete(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[DEBUG] Deleting deployment %s\n", d.Id())
	releaseArgs := gomaasapi.ReleaseMachinesArgs{
		SystemIDs: []string{d.Id()},
	}

	if erase, ok := d.GetOk("release_erase"); ok {
		releaseArgs.Erase = erase.(bool)
	}

	if eraseSecure, ok := d.GetOk("release_erase_secure"); ok {
		// setting erase to true in the event a user didn't set both options
		releaseArgs.Erase = true
		releaseArgs.SecureErase = eraseSecure.(bool)
	}

	if eraseQuick, ok := d.GetOk("release_erase_quick"); ok {
		// setting erase to true in the event a user didn't set both options
		releaseArgs.Erase = true
		releaseArgs.QuickErase = eraseQuick.(bool)
	}

	controller := meta.(*Config).Controller
	if err := controller.ReleaseMachines(releaseArgs); err != nil {
		return err
	}

	stateConf := &resource.StateChangeConf{
		Pending:    []string{"Deployed", "Releasing", "Disk erasing"},
		Target:     []string{"Ready"},
		Refresh:    getMachineStatus(controller, d.Id()),
		Timeout:    30 * time.Minute,
		Delay:      10 * time.Second,
		MinTimeout: 3 * time.Second,
	}

	if _, err := stateConf.WaitForState(); err != nil {
		return fmt.Errorf(
			"[ERROR] [resourceMAASDeploymentCreate] Error waiting for deployment (%s) to become ready: %s", d.Id(), err)
	}

	// remove deploy hostname if set
	// note: we can't do this with the current gomaasapi because empty strings are not added
	//if _, ok := d.GetOk("deploy_hostname"); ok {
	//	params := url.Values{}
	//	params.Set("hostname", "")
	//	err := nodeUpdate(meta.(*Config).MAASObject, d.Id(), params)
	//	if err != nil {
	//		log.Printf("[DEBUG] Unable to reset hostname: %v", err)
	//	}
	//}

	// remove deployed tags
	if tags, ok := d.GetOk("deploy_tags"); ok {
		for i := range tags.([]interface{}) {
			tag, err := controller.GetTag(tags.([]interface{})[i].(string))
			if err != nil {
				log.Printf("[ERROR] Unable to update node (%s) with tag (%s)", d.Id(), tags.([]interface{})[i].(string))
			}
			err = tag.RemoveFromMachine(d.Id())
			if err != nil {
				log.Printf("[ERROR] Unable to update node (%s) with tag (%s)", d.Id(), tags.([]interface{})[i].(string))
			}
		}
	}

	log.Printf("[DEBUG] [resourceMAASDeploymentDelete] Node (%s) released", d.Id())

	d.SetId("")

	return nil
}
