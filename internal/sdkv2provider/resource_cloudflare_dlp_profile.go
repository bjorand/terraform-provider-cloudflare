package sdkv2provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudflare/cloudflare-go"
	"github.com/cloudflare/terraform-provider-cloudflare/internal/consts"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func resourceCloudflareDLPProfile() *schema.Resource {
	return &schema.Resource{
		Schema:        resourceCloudflareDLPProfileSchema(),
		CreateContext: resourceCloudflareDLPProfileCreate,
		ReadContext:   resourceCloudflareDLPProfileRead,
		UpdateContext: resourceCloudflareDLPProfileUpdate,
		DeleteContext: resourceCloudflareDLPProfileDelete,
		Importer: &schema.ResourceImporter{
			StateContext: resourceCloudflareDLPProfileImport,
		},
		Description: heredoc.Doc(`
			Provides a Cloudflare DLP Profile resource. Data Loss Prevention profiles
			are a set of entries that can be matched in HTTP bodies or files.
			They are referenced in Zero Trust Gateway rules.
		`),
	}
}

func dlpPatternToSchema(pattern cloudflare.DLPPattern) map[string]interface{} {
	schema := make(map[string]interface{})
	if pattern.Regex != "" {
		schema["regex"] = pattern.Regex
	}
	if pattern.Validation != "" {
		schema["validation"] = pattern.Validation
	}
	return schema
}

func dlpPatternToAPI(pattern map[string]interface{}) cloudflare.DLPPattern {
	entryPattern := cloudflare.DLPPattern{
		Regex: pattern["regex"].(string),
	}
	if validation, ok := pattern["validation"].(string); ok {
		entryPattern.Validation = validation
	}
	return entryPattern
}

func dlpEntryToSchema(entry cloudflare.DLPEntry) map[string]interface{} {
	entrySchema := make(map[string]interface{})
	if entry.ID != "" {
		entrySchema["id"] = entry.ID
	}
	if entry.Name != "" {
		entrySchema["name"] = entry.Name
	}
	entrySchema["enabled"] = entry.Enabled != nil && *entry.Enabled == true
	if entry.Pattern != nil {
		entrySchema["pattern"] = []interface{}{dlpPatternToSchema(*entry.Pattern)}
	}
	return entrySchema
}

func dlpEntryToAPI(entryType string, entryMap map[string]interface{}) cloudflare.DLPEntry {
	apiEntry := cloudflare.DLPEntry{
		Name: entryMap["name"].(string),
	}
	if entryID, ok := entryMap["id"].(string); ok {
		apiEntry.ID = entryID
	}
	if patterns, ok := entryMap["pattern"].([]interface{}); ok && len(patterns) != 0 {
		newPattern := dlpPatternToAPI(patterns[0].(map[string]interface{}))
		apiEntry.Pattern = &newPattern
	}
	enabled := entryMap["enabled"] == true
	apiEntry.Enabled = &enabled
	apiEntry.Type = entryType
	return apiEntry
}

func resourceCloudflareDLPProfileRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*cloudflare.API)

	identifier := cloudflare.AccountIdentifier(d.Get(consts.AccountIDSchemaKey).(string))
	dlpProfile, err := client.GetDLPProfile(ctx, identifier, d.Id())
	var notFoundError *cloudflare.NotFoundError
	if errors.As(err, &notFoundError) {
		tflog.Info(ctx, fmt.Sprintf("DLP Profile %s no longer exists", d.Id()))
		d.SetId("")
		return nil
	}
	if err != nil {
		return diag.FromErr(fmt.Errorf("error reading DLP profile: %w", err))
	}

	d.Set("name", dlpProfile.Name)
	d.Set("type", dlpProfile.Type)
	if dlpProfile.Description != "" {
		d.Set("description", dlpProfile.Description)
	}
	entries := make([]interface{}, 0, len(dlpProfile.Entries))
	for _, entry := range dlpProfile.Entries {
		entries = append(entries, dlpEntryToSchema(entry))
	}
	d.Set("entry", schema.NewSet(schema.HashResource(&schema.Resource{
		Schema: resourceCloudflareDLPEntrySchema(),
	}), entries))

	return nil
}

func resourceCloudflareDLPProfileCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*cloudflare.API)
	identifier := cloudflare.AccountIdentifier(d.Get(consts.AccountIDSchemaKey).(string))

	newDLPProfile := cloudflare.DLPProfile{
		Name:        d.Get("name").(string),
		Type:        d.Get("type").(string),
		Description: d.Get("description").(string),
	}

	if newDLPProfile.Type == DLPProfileTypePredefined {
		return diag.FromErr(fmt.Errorf("predefined DLP profiles cannot be created and must be imported"))
	}

	if entries, ok := d.GetOk("entry"); ok {
		for _, entry := range entries.(*schema.Set).List() {
			newDLPProfile.Entries = append(newDLPProfile.Entries, dlpEntryToAPI(newDLPProfile.Type, entry.(map[string]interface{})))
		}
	}

	dlpProfiles, err := client.CreateDLPProfiles(ctx, identifier, cloudflare.CreateDLPProfilesParams{
		Profiles: []cloudflare.DLPProfile{newDLPProfile},
		Type:     newDLPProfile.Type,
	})
	if err != nil {
		return diag.FromErr(fmt.Errorf("error creating DLP Profile for name %s: %w", newDLPProfile.Name, err))
	}
	if len(dlpProfiles) == 0 {
		return diag.FromErr(fmt.Errorf("error creating DLP Profile for name %s: no profile in response", newDLPProfile.Name))
	}

	d.SetId(dlpProfiles[0].ID)
	return resourceCloudflareDLPProfileRead(ctx, d, meta)
}

func resourceCloudflareDLPProfileUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*cloudflare.API)

	updatedDLPProfile := cloudflare.DLPProfile{
		ID:   d.Id(),
		Name: d.Get("name").(string),
		Type: d.Get("type").(string),
	}
	updatedDLPProfile.Description, _ = d.Get("description").(string)
	if entries, ok := d.GetOk("entry"); ok {
		for _, entry := range entries.(*schema.Set).List() {
			updatedDLPProfile.Entries = append(updatedDLPProfile.Entries, dlpEntryToAPI(updatedDLPProfile.Type, entry.(map[string]interface{})))
		}
	}

	tflog.Debug(ctx, fmt.Sprintf("Updating Cloudflare DLP Profile from struct: %+v", updatedDLPProfile))

	identifier := cloudflare.AccountIdentifier(d.Get(consts.AccountIDSchemaKey).(string))
	dlpProfile, err := client.UpdateDLPProfile(ctx, identifier, cloudflare.UpdateDLPProfileParams{
		ProfileID: updatedDLPProfile.ID,
		Profile:   updatedDLPProfile,
		Type:      updatedDLPProfile.Type,
	})
	if err != nil {
		return diag.FromErr(fmt.Errorf("error updating DLP profile for ID %q: %w", d.Id(), err))
	}
	if dlpProfile.ID == "" {
		return diag.FromErr(fmt.Errorf("failed to find DLP Profile ID in update response; resource was empty"))
	}

	return resourceCloudflareDLPProfileRead(ctx, d, meta)
}

func resourceCloudflareDLPProfileDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*cloudflare.API)
	tflog.Debug(ctx, fmt.Sprintf("Deleting Cloudflare DLP Profile using ID: %s", d.Id()))

	profileType, _ := d.Get("type").(string)
	if profileType != DLPProfileTypeCustom {
		return diag.FromErr(fmt.Errorf("error deleting DLP Profile: can only delete custom profiles"))
	}
	identifier := cloudflare.AccountIdentifier(d.Get(consts.AccountIDSchemaKey).(string))
	if err := client.DeleteDLPProfile(ctx, identifier, d.Id()); err != nil {
		return diag.FromErr(fmt.Errorf("error deleting DLP Profile for ID %q: %w", d.Id(), err))
	}

	resourceCloudflareDLPProfileRead(ctx, d, meta)
	return nil
}

func resourceCloudflareDLPProfileImport(ctx context.Context, d *schema.ResourceData, meta interface{}) ([]*schema.ResourceData, error) {
	attributes := strings.Split(d.Id(), "/")
	if len(attributes) != 2 {
		return nil, fmt.Errorf(
			"invalid id (%q) specified, should be in format %q",
			d.Id(),
			"accountID/dlpProfileID",
		)
	}
	accountID, dlpProfileID := attributes[0], attributes[1]

	tflog.Debug(ctx, fmt.Sprintf("Importing Cloudflare DLP Profile: %q, ID %q", accountID, dlpProfileID))

	d.Set("account_id", accountID)
	d.SetId(dlpProfileID)

	resourceCloudflareDLPProfileRead(ctx, d, meta)
	return []*schema.ResourceData{d}, nil
}
