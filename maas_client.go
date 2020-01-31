package main

import (
	"log"

	"github.com/hashicorp/terraform/helper/resource"
	"github.com/juju/gomaasapi"
)

func getMachineStatus(controller gomaasapi.Controller, systemID string) resource.StateRefreshFunc {
	log.Printf("[DEBUG] [getNodeStatus] Getting stat of node: %s", systemID)
	return func() (interface{}, string, error) {
		machine, err := controller.GetMachine(systemID)
		if err != nil {
			log.Printf("[ERROR] [getNodeStatus] Unable to get node: %s\n", systemID)
			return nil, "", err
		}
		return machine, machine.StatusName(), nil
	}
}
