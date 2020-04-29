package main

import (
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/hashicorp/terraform/helper/schema"
	"github.com/juju/gomaasapi"
)

func updateMachineBlockDevices(d *schema.ResourceData, controller gomaasapi.Controller, machine gomaasapi.Machine) error {
	// Create a mapping of physical block devices
	nameToDevice := map[string]gomaasapi.BlockDevice{}

	// Get the current volume groups
	vgs, err := machine.VolumeGroups()
	if err != nil {
		return err
	}

	// Delete any logical volumes
	for _, blockDevice := range machine.BlockDevices() {
		// Check to see if this volume belongs to a volume group.
		for _, vg := range vgs {
			if !strings.HasPrefix(blockDevice.Name(), fmt.Sprintf("%s-", vg.Name())) {
				continue
			}
			log.Printf("[DEBUG] [updateMachineBlockDevices] Deleting logical volume block device '%s'", blockDevice.Name())
			err := blockDevice.Delete()
			if err != nil {
				return err
			}
		}
	}

	// Delete any volume groups
	for _, vg := range vgs {
		log.Printf("[DEBUG] [updateMachineBlockDevices] Found volume group '%s' with uuid '%v'", vg.Name(), vg.UUID())
		log.Printf("[DEBUG] [updateMachineBlockDevices] Deleting volume group '%s'", vg.Name())
		err := vg.Delete()
		if err != nil {
			return err
		}
	}

	// Clear all partitions from all physical devices.
	for _, blockDevice := range machine.BlockDevices() {
		log.Printf("[DEBUG] [updateMachineBlockDevices] Found block device '%s' with id '%v'", blockDevice.Name(), blockDevice.ID())
		for _, partition := range blockDevice.Partitions() {
			log.Printf("[DEBUG] [updateMachineBlockDevices] Found partition '%s' with id '%v'", partition.Path(), partition.ID())
			log.Printf("[DEBUG] [updateMachineBlockDevices] Deleting partition '%s'", partition.Path())
			err := partition.Delete()
			if err != nil {
				return err
			}
		}
		nameToDevice[blockDevice.Name()] = blockDevice
	}

	// Re-create the volume configuration based on the resource data.
	// Loop through all defined block devices
	partitionsMap := map[string]gomaasapi.Partition{}
	blockDevicesDef := d.Get("block_device").(*schema.Set)
	for _, item := range blockDevicesDef.List() {
		deviceDef := item.(map[string]interface{})
		name := deviceDef["name"].(string)
		blockDevice, ok := nameToDevice[name]
		if !ok {
			return fmt.Errorf("block device '%s' was not found in MAAS machine '%s'", name, machine.SystemID())
		}
		deviceParts, err := createMachinePartitions(deviceDef, blockDevice)
		if err != nil {
			return err
		}

		// Add the created partitions to our map of partitions.
		for key, value := range deviceParts {
			partitionsMap[key] = value
		}
	}

	// Re-create the volume group configuration based on the resource data.
	vgsDef := d.Get("volume_group").(*schema.Set)
	for _, item := range vgsDef.List() {
		vgDef := item.(map[string]interface{})
		name := vgDef["name"].(string)
		devices := vgDef["devices"].(*schema.Set)

		// Loop through the defined devices that belong to this volume
		// group and look up its partition.
		// TODO: Handle looking for block devices as well as partitions
		partitions := []gomaasapi.Partition{}
		for _, deviceDef := range devices.List() {
			device := deviceDef.(string)
			partition, ok := partitionsMap[device]
			if !ok {
				return fmt.Errorf("required partition '%s' for volume group '%s' was not found", device, name)
			}

			// Ensure that the specified partition is of fstype "lvm-pv"
			if partition.FileSystem() != nil {
				fsType := partition.FileSystem().Type()
				return fmt.Errorf("expected lvm partition '%s' to be formatted as 'lvm-pv', not '%s'", device, fsType)
			}

			partitions = append(partitions, partition)
		}

		// Create the volume group
		log.Printf("[DEBUG] [updateMachineBlockDevices] Creating volume group '%s'", name)
		vg, err := machine.CreateVolumeGroup(gomaasapi.CreateVolumeGroupArgs{
			Name:       name,
			Partitions: partitions,
		})
		if err != nil {
			return err
		}

		// Create logical volumes on the volume group
		lvs := vgDef["logical_volume"].(*schema.Set)
		for _, l := range lvs.List() {
			lvDef := l.(map[string]interface{})
			name := lvDef["name"].(string)
			size := lvDef["size"].(int)
			fsType := lvDef["fstype"].(string)
			mountpoint := lvDef["mountpoint"].(string)

			// Ensure that the logical volume is named correctly.
			if !strings.HasPrefix(name, fmt.Sprintf("%s-", vg.Name())) {
				return fmt.Errorf("logical volume '%s' must have a name that starts with '%s-'", name, vg.Name())
			}

			// Create the logical volume
			log.Printf("[DEBUG] [updateMachineBlockDevices] Creating logical volume '%s' on '%s'", name, vg.Name())
			lv, err := vg.CreateLogicalVolume(gomaasapi.CreateLogicalVolumeArgs{
				Name: strings.Replace(name, fmt.Sprintf("%s-", vg.Name()), "", 1),
				Size: size,
			})
			if err != nil {
				return err
			}

			// Format the logical volume with the given filesystem
			if fsType == "" {
				continue
			}
			log.Printf("[DEBUG] [updateMachineBlockDevices] Formatting logical volume '%s' as '%s'", name, fsType)
			err = lv.Format(gomaasapi.FormatStorageDeviceArgs{
				FSType: fsType,
			})
			if err != nil {
				return err
			}

			// Mount the logical volume to the given path
			if mountpoint == "" {
				continue
			}
			log.Printf("[DEBUG] [updateMachineBlockDevices] Mounting logical volume '%s' at '%s'", name, mountpoint)
			err = lv.Mount(gomaasapi.MountStorageDeviceArgs{
				MountPoint: mountpoint,
			})
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func createMachinePartitions(deviceDef map[string]interface{}, blockDevice gomaasapi.BlockDevice) (map[string]gomaasapi.Partition, error) {
	partitions := map[string]gomaasapi.Partition{}
	devicePath := deviceDef["path"].(string)
	partitionSet := deviceDef["partition"].(*schema.Set)

	// Sort the list of partitions so they are created in the correct order
	// e.g. sda1 creates before sda2
	partitionsMap := map[string]map[string]interface{}{}
	partitionPaths := []string{}
	for _, item := range partitionSet.List() {
		partitionDef := item.(map[string]interface{})
		path := partitionDef["path"].(string)
		partitionPaths = append(partitionPaths, path)
		partitionsMap[path] = partitionDef
	}
	sort.Strings(partitionPaths)

	// Create partitions on the given block device.
	for _, path := range partitionPaths {
		partitionDef := partitionsMap[path]
		size := partitionDef["size"].(int)
		fstype := partitionDef["fstype"].(string)

		// If this is an LVM partition, don't format it. Formatting
		// is handled upon Volume Group creation.
		if fstype == "lvm-pv" {
			fstype = ""
		}

		// Ensure that the defined partition path belongs to
		// the block device we're creating this on.
		if !strings.Contains(path, devicePath) {
			return nil, fmt.Errorf("partition '%s' incorrectly defined on device '%s'", path, devicePath)
		}

		// Create the partition with the given size
		log.Printf("[DEBUG] [updateMachineBlockDevices] Creating partition '%s' on '%s'", path, devicePath)
		partition, err := blockDevice.CreatePartition(gomaasapi.CreatePartitionArgs{
			Size: size,
		})
		if err != nil {
			return nil, err
		}

		// Format the partition with the given filesystem, if one is defined.
		if fstype != "" {
			log.Printf("[DEBUG] [updateMachineBlockDevices] Formatting partition '%s' as '%s'", path, fstype)
			err = partition.Format(gomaasapi.FormatStorageDeviceArgs{
				FSType: fstype,
			})
			if err != nil {
				return nil, err
			}
		}

		// Mount the partition if one is defined.
		mountpoint := partitionDef["mountpoint"].(string)
		if mountpoint != "" {
			log.Printf("[DEBUG] [updateMachineBlockDevices] Mounting partition '%s' to '%s'", path, mountpoint)
			err = partition.Mount(gomaasapi.MountStorageDeviceArgs{
				MountPoint: mountpoint,
			})
			if err != nil {
				return nil, err
			}
		}
		partitions[path] = partition
	}
	return partitions, nil
}
func buildVolumeGroup(machine gomaasapi.Machine, vg gomaasapi.VolumeGroup) map[string]interface{} {
	volumeGroupTf := map[string]interface{}{}
	volumeGroupTf["name"] = vg.Name()
	volumeGroupTf["size"] = vg.Size()

	// Get the physical volumes that comprise this volume group.
	devices := []string{}
	for _, device := range vg.Devices() {
		devices = append(devices, device.Path())
	}
	volumeGroupTf["devices"] = devices

	// Find the logical volumes that are part of this volume group.
	logicalVolumes := []map[string]interface{}{}
	for _, device := range machine.BlockDevices() {
		// TODO: Figure out how to handle cases where we could falsely
		// find a device that starts with the volume group name.
		if strings.HasPrefix(device.Name(), fmt.Sprintf("%s-", vg.Name())) {
			log.Printf("[DEBUG] Found logical device %v", device)
			logicalVolumes = append(logicalVolumes, buildLogicalVolume(device))
		}
	}
	volumeGroupTf["logical_volume"] = logicalVolumes

	return volumeGroupTf
}

func buildLogicalVolume(device gomaasapi.BlockDevice) map[string]interface{} {
	deviceTf := map[string]interface{}{}
	deviceTf["name"] = device.Name()
	deviceTf["size"] = device.Size()

	// There are certain situations where device.FileSystem() will return
	// a nil reference and panic, but comparing device.FileSystem() to
	// nil returns false. This block catches that.
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[DEBUG] Device '%s' has no filesystem defined.", device.Name())
			}
		}()
		deviceTf["fstype"] = device.FileSystem().Type()
		mountPoint := device.FileSystem().MountPoint()
		if mountPoint != "" {
			deviceTf["mountpoint"] = mountPoint
		}
	}()

	return deviceTf
}

func buildBlockDevice(device gomaasapi.BlockDevice) map[string]interface{} {
	deviceTf := map[string]interface{}{}
	deviceTf["name"] = device.Name()
	deviceTf["id_path"] = device.IDPath()
	// outputs
	deviceTf["uuid"] = device.UUID()
	deviceTf["path"] = device.Path()
	deviceTf["model"] = device.Model()
	deviceTf["size"] = device.Size()
	deviceTf["block_size"] = device.BlockSize()

	// Create partitions
	partitions := []map[string]interface{}{}
	for _, partition := range device.Partitions() {
		partitions = append(partitions, buildPartition(partition))
	}
	deviceTf["partition"] = partitions
	return deviceTf
}

func buildPartition(partition gomaasapi.Partition) map[string]interface{} {
	partitionTf := map[string]interface{}{}
	partitionTf["path"] = partition.Path()
	partitionTf["size"] = partition.Size()
	if partition.FileSystem() != nil {
		partitionTf["fstype"] = partition.FileSystem().Type()
		mountPoint := partition.FileSystem().MountPoint()
		if mountPoint != "" {
			partitionTf["mountpoint"] = mountPoint
		}
	}

	return partitionTf
}
