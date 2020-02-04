package main

import (
	"crypto/sha1"
	"encoding/hex"
	"log"

	"github.com/hashicorp/terraform/helper/schema"
)

// resourceMAASDeployment creates a new terraform schema resource
func resourceMAASDeployment() *schema.Resource {
	log.Println("[DEBUG] [resourceMAASDeployment] Initializing data structure")
	return &schema.Resource{
		Create: resourceMAASDeploymentCreate,
		Read:   resourceMAASDeploymentRead,
		Update: resourceMAASDeploymentUpdate,
		Delete: resourceMAASDeploymentDelete,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},

		SchemaVersion: 1,

		Schema: map[string]*schema.Schema{
			"architecture": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
			},

			"boot_type": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
			},

			"cpu_count": {
				Type:     schema.TypeInt,
				Optional: true,
				ForceNew: true,
			},

			"disable_ipv4": {
				Type:     schema.TypeBool,
				Optional: true,
			},

			"distro_series": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
			},

			"hostname": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
			},

			"tags": {
				Type:     schema.TypeList,
				Optional: true,
				ForceNew: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
			},

			"release_erase": {
				Type:     schema.TypeBool,
				Optional: true,
				ForceNew: false,
				Default:  true,
			},

			"release_erase_secure": {
				Type:     schema.TypeBool,
				Optional: true,
				ForceNew: false,
				Default:  false,
			},

			"release_erase_quick": {
				Type:     schema.TypeBool,
				Optional: true,
				ForceNew: false,
				Default:  false,
			},
			/*
				"ip_addresses": {
					Type:     schema.TypeList,
					Optional: true,
					ForceNew: true,
					Elem:     &schema.Schema{Type: schema.TypeString},
				},

				"macaddress_set": {
					Type:     schema.TypeList,
					Optional: true,
					ForceNew: true,
					Elem: &schema.Resource{
						Schema: map[string]*schema.Schema{
							"mac_address": {
								Type:     schema.TypeString,
								Optional: true,
								ForceNew: true,
							},
							"resource_uri": {
								Type:     schema.TypeString,
								Optional: true,
								ForceNew: true,
							},
						},
					},
				},
			*/
			"memory": {
				Type:     schema.TypeInt,
				Optional: true,
				ForceNew: true,
			},
			/*
				"netboot": {
					Type:     schema.TypeBool,
					Optional: true,
					ForceNew: true,
				},
			*/
			"osystem": {
				Type:     schema.TypeString,
				Computed: true,
			},
			/*
				"owner": {
					Type:     schema.TypeString,
					Optional: true,
					ForceNew: true,
				},

				"physicalblockdevice_set": {
					Type:     schema.TypeList,
					Optional: true,
					ForceNew: true,
					Elem: &schema.Resource{
						Schema: map[string]*schema.Schema{
							"block_size": {
								Type:     schema.TypeInt,
								Optional: true,
							},
							"id": {
								Type:     schema.TypeInt,
								Optional: true,
							},
							"id_path": {
								Type:     schema.TypeString,
								Optional: true,
							},
							"model": {
								Type:     schema.TypeString,
								Optional: true,
							},
							"name": {
								Type:     schema.TypeString,
								Optional: true,
							},
							"path": {
								Type:     schema.TypeString,
								Optional: true,
							},
							"serial": {
								Type:     schema.TypeString,
								Optional: true,
							},
							"size": {
								Type:     schema.TypeInt,
								Optional: true,
							},
							"tags": {
								Type:     schema.TypeList,
								Optional: true,
								Elem:     &schema.Schema{Type: schema.TypeString},
							},
						},
					},
				},
				"power_state": {
					Type:     schema.TypeString,
					Optional: true,
				},
				"resource_uri": {
					Type:     schema.TypeString,
					Computed: true,
				},
				"routers": {
					Type:     schema.TypeList,
					Optional: true,
					Elem:     &schema.Schema{Type: schema.TypeString},
				},
				"status": {
					Type:     schema.TypeInt,
					Optional: true,
				},
				"storage": {
					Type:     schema.TypeInt,
					Optional: true,
				},
				"swap_size": {
					Type:     schema.TypeInt,
					Optional: true,
				},
				"tag_names": {
					Type:     schema.TypeList,
					Optional: true,
					Elem:     &schema.Schema{Type: schema.TypeString},
				},

				"zone": {
					Type:     schema.TypeSet,
					Optional: true,
					Elem: &schema.Resource{
						Schema: map[string]*schema.Schema{
							"description": {
								Type:     schema.TypeString,
								Optional: true,
							},
							"name": {
								Type:     schema.TypeString,
								Optional: true,
							},
							"resource_uri": {
								Type:     schema.TypeString,
								Optional: true,
							},
						},
					},
				},
			*/
			"user_data": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
				StateFunc: func(v interface{}) string {
					switch v.(type) {
					case string:
						hash := sha1.Sum([]byte(v.(string)))
						return hex.EncodeToString(hash[:])
					default:
						return ""
					}
				},
			},

			"hwe_kernel": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
			},

			"comment": {
				Type:     schema.TypeString,
				Optional: true,
			},
		},
	}
}

// resourceMAASMachine creates a new terraform schema resource
func resourceMAASMachine() *schema.Resource {
	log.Println("[DEBUG] [resourceMAASMachine] Initializing data structure")
	return &schema.Resource{
		Create: resourceMAASMachineCreate,
		Read:   resourceMAASMachineRead,
		Update: resourceMAASMachineUpdate,
		Delete: resourceMAASMachineDelete,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},
		SchemaVersion: 1,

		Schema: map[string]*schema.Schema{
			"architecture": {
				Type:     schema.TypeString,
				Optional: true,
				Default:  "amd64",
				ForceNew: true,
			},
			"mac_address": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"domain": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: false,
			},
			"description": {
				Type:     schema.TypeString,
				Optional: true,
				Default:  "",
			},
			"hostname": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: false,
			},
			"tags": {
				Type:     schema.TypeList,
				Optional: true,
				ForceNew: false,
				Elem:     &schema.Schema{Type: schema.TypeString},
			},
			"enable_ssh": {
				Type:     schema.TypeBool,
				Optional: true,
				ForceNew: true,
				Default:  false,
			},
			"skip_bmc_config": {
				Type:     schema.TypeBool,
				Optional: true,
				ForceNew: true,
				Default:  false,
			},
			"skip_networking": {
				Type:     schema.TypeBool,
				Optional: true,
				ForceNew: true,
				Default:  false,
			},
			"skip_storage": {
				Type:     schema.TypeBool,
				Optional: true,
				ForceNew: true,
				Default:  false,
			},
			"commissioning_scripts": {
				Type:     schema.TypeList,
				Optional: true,
				ForceNew: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
			},
			"testing_scripts": {
				Type:     schema.TypeList,
				Optional: true,
				ForceNew: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
			},
			"interface": {
				Type:     schema.TypeList,
				Optional: true,
				ForceNew: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"name": {
							Type:     schema.TypeString,
							Required: true,
						},
						"subnet": {
							Type:     schema.TypeString,
							Required: true,
						},
						"mode": {
							Type:     schema.TypeString,
							Required: true,
						},
						//"vlan": {
						//	Type:     schema.TypeInt,
						//	Required: true,
						//},
						//"tags": {
						//	Type:     schema.TypeList,
						//	Optional: true,
						//	Elem:     &schema.Schema{Type: schema.TypeString},
						//},
						"bond": {
							Type:     schema.TypeList,
							Optional: true,
							ForceNew: true,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"parents": {
										Type:     schema.TypeList,
										Required: true,
										Elem:     &schema.Schema{Type: schema.TypeString},
									},
									"mac_address": {
										Type:     schema.TypeString,
										Optional: true,
									},
									"miimon": {
										Type:     schema.TypeInt,
										Optional: true,
									},
									"downdelay": {
										Type:     schema.TypeInt,
										Optional: true,
									},
									"updelay": {
										Type:     schema.TypeInt,
										Optional: true,
									},
									"lacp_rate": {
										Type:     schema.TypeString,
										Optional: true,
									},
									"xmit_hash_policy": {
										Type:     schema.TypeString,
										Optional: true,
									},
									"mode": {
										Type:     schema.TypeString,
										Optional: true,
									},
								},
								// -- vlan --
								// tags
								// vlan
								// parent
								// --bridge --
								// name
								// mac_address
								// tags
								// vlan
								// parent
								// bridge_stp
								// bridge_fd
								// -- ipv6 --
								// accept_ra
								// autoconf
							},
						},
					},
				},
			},

			"power": {
				Type:     schema.TypeList,
				Optional: true,
				ForceNew: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"type": {
							Type:     schema.TypeString,
							Required: true,
						},
						"user": {
							Type:     schema.TypeString,
							Optional: true,
						},
						"password": {
							Type:     schema.TypeString,
							Optional: true,
						},
						"address": {
							Type:     schema.TypeString,
							Required: true,
						},
						"custom": {
							Type:     schema.TypeMap,
							Optional: true,
							Elem:     &schema.Schema{Type: schema.TypeString},
						},
					},
				},
			},
		},
	}
}

// TODO: add maas_node schema
