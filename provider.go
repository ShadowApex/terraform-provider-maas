package main

import (
	"log"

	"github.com/hashicorp/terraform/helper/schema"
	"github.com/hashicorp/terraform/terraform"
)

// Provider creates the schema for the provider config
func Provider() terraform.ResourceProvider {
	log.Println("[DEBUG] Initializing the MAAS provider")
	return &schema.Provider{
		Schema: map[string]*schema.Schema{
			"api_key": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "The admin-level api key for machine configuration",
			},
			"api_url": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "The MAAS server URL. ie: http://1.2.3.4:80/MAAS",
			},
			"api_version": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "2.0",
				Description: "The MAAS API version. Currently: 1.0",
			},
			"api_deploy_tokens": {
				Type:        schema.TypeMap,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Optional:    true,
				Default:     map[string]interface{}{},
				Description: "A mapping of username to API tokens for deploying with owner=xxxx",
			},
		},

		ResourcesMap: map[string]*schema.Resource{
			"maas_deployment": resourceMAASDeployment(),
			"maas_machine":    resourceMAASMachine(),
		},

		ConfigureFunc: providerConfigure,
	}
}

// providerConfigure loads in the provider configuration
func providerConfigure(d *schema.ResourceData) (interface{}, error) {
	log.Println("[DEBUG] Configuring the MAAS provider")

	config := Config{
		APIKey:       d.Get("api_key").(string),
		APIURL:       d.Get("api_url").(string),
		APIver:       d.Get("api_version").(string),
		DeployTokens: map[string]string{},
	}

	for k, v := range d.Get("api_deploy_tokens").(map[string]interface{}) {
		config.DeployTokens[k] = v.(string)
	}
	return config.Client()
}
