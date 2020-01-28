package main

import (
	"log"

	"github.com/juju/gomaasapi"
)

func machineUpdateTags(controller gomaasapi.Controller, machine gomaasapi.Machine, name string) error {
	tag, err := controller.GetTag(name)
	if err != nil {
		log.Printf("[ERROR] [nodeTagsUpdate] Tag %s does not exist, creating...", name)
		tag, err = controller.CreateTag(gomaasapi.CreateTagArgs{Name: name, Comment: "added by terraform", Definition: ""})
		if err != nil {
			return err
		}
	}

	log.Printf("[ERROR] [nodeTagsUpdate] Adding tag %s to %s", name, machine.FQDN())
	return tag.AddToMachine(machine.SystemID())
}
