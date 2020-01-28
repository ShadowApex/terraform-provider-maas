package main

import (
	"log"

	"github.com/hashicorp/terraform/helper/resource"
	"github.com/juju/gomaasapi"
)

func getMachineStatus(controller gomaasapi.Controller, systemID string) resource.StateRefreshFunc {
	log.Printf("[DEBUG] [getNodeStatus] Getting stat of node: %s", systemID)
	return func() (interface{}, string, error) {
		machines, err := controller.Machines(gomaasapi.MachinesArgs{SystemIDs: []string{systemID}})
		if err != nil || len(machines) == 0 {
			log.Printf("[ERROR] [getNodeStatus] Unable to get node: %s\n", systemID)
			return nil, "", err
		}
		return machines[0], machines[0].StatusName(), nil
	}
}
