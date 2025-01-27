package tfe

import (
	"fmt"
	"log"

	tfe "github.com/hashicorp/go-tfe"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func resourceTFETerraformVersion() *schema.Resource {
	return &schema.Resource{
		Create: resourceTFETerraformVersionCreate,
		Read:   resourceTFETerraformVersionRead,
		Update: resourceTFETerraformVersionUpdate,
		Delete: resourceTFETerraformVersionDelete,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},

		Schema: map[string]*schema.Schema{
			"version": {
				Type:     schema.TypeString,
				Required: true,
			},
			"url": {
				Type:     schema.TypeString,
				Required: true,
			},
			"sha": {
				Type:     schema.TypeString,
				Required: true,
			},
			"official": {
				Type:     schema.TypeBool,
				Optional: true,
				Default:  false,
			},
			"enabled": {
				Type:     schema.TypeBool,
				Optional: true,
				Default:  true,
			},
			"beta": {
				Type:     schema.TypeBool,
				Optional: true,
				Default:  false,
			},
		},
	}
}

func resourceTFETerraformVersionCreate(d *schema.ResourceData, meta interface{}) error {
	tfeClient := meta.(*tfe.Client)

	opts := tfe.AdminTerraformVersionCreateOptions{
		Version:  tfe.String(d.Get("version").(string)),
		URL:      tfe.String(d.Get("url").(string)),
		Sha:      tfe.String(d.Get("sha").(string)),
		Official: tfe.Bool(d.Get("official").(bool)),
		Enabled:  tfe.Bool(d.Get("enabled").(bool)),
		Beta:     tfe.Bool(d.Get("beta").(bool)),
	}

	log.Printf("[DEBUG] Create new Terraform version: %s", *opts.Version)
	v, err := tfeClient.Admin.TerraformVersions.Create(ctx, opts)
	if err != nil {
		return fmt.Errorf("Error creating the new Terraform version %s: %v", *opts.Version, err)
	}

	d.SetId(v.ID)

	return resourceTFETerraformVersionUpdate(d, meta)
}

func resourceTFETerraformVersionRead(d *schema.ResourceData, meta interface{}) error {
	tfeClient := meta.(*tfe.Client)

	log.Printf("[DEBUG] Read configuration of Terraform version: %s", d.Id())
	v, err := tfeClient.Admin.TerraformVersions.Read(ctx, d.Id())
	if err != nil {
		if err == tfe.ErrResourceNotFound {
			log.Printf("[DEBUG] Terraform version %s does no longer exist", d.Id())
			d.SetId("")
			return nil
		}
		return err
	}

	d.Set("version", v.Version)
	d.Set("url", v.URL)
	d.Set("sha", v.Sha)
	d.Set("official", v.Official)
	d.Set("enabled", v.Enabled)
	d.Set("beta", v.Beta)

	return nil
}

func resourceTFETerraformVersionUpdate(d *schema.ResourceData, meta interface{}) error {
	tfeClient := meta.(*tfe.Client)

	opts := tfe.AdminTerraformVersionUpdateOptions{
		Version:  tfe.String(d.Get("version").(string)),
		URL:      tfe.String(d.Get("url").(string)),
		Sha:      tfe.String(d.Get("sha").(string)),
		Official: tfe.Bool(d.Get("official").(bool)),
		Enabled:  tfe.Bool(d.Get("enabled").(bool)),
		Beta:     tfe.Bool(d.Get("beta").(bool)),
	}

	log.Printf("[DEBUG] Update configuration of Terraform version: %s", d.Id())
	v, err := tfeClient.Admin.TerraformVersions.Update(ctx, d.Id(), opts)
	if err != nil {
		return fmt.Errorf("Error updating Terraform version %s: %v", d.Id(), err)
	}

	d.SetId(v.ID)

	return resourceTFETerraformVersionRead(d, meta)
}

func resourceTFETerraformVersionDelete(d *schema.ResourceData, meta interface{}) error {
	tfeClient := meta.(*tfe.Client)

	log.Printf("[DEBUG] Delete Terraform version: %s", d.Id())
	err := tfeClient.Admin.TerraformVersions.Delete(ctx, d.Id())
	if err != nil {
		if err == tfe.ErrResourceNotFound {
			return nil
		}
		return fmt.Errorf("Error deleting Terraform version %s: %v", d.Id(), err)
	}

	return nil
}
