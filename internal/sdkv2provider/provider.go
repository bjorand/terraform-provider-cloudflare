package sdkv2provider

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	cloudflare "github.com/cloudflare/cloudflare-go"
	"github.com/cloudflare/terraform-provider-cloudflare/internal/consts"
	"github.com/cloudflare/terraform-provider-cloudflare/internal/utils"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/logging"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/hashicorp/terraform-plugin-sdk/v2/meta"
)

func init() {
	schema.DescriptionKind = schema.StringMarkdown

	schema.SchemaDescriptionBuilder = func(s *schema.Schema) string {
		desc := s.Description
		desc = strings.TrimSpace(desc)

		if !bytes.HasSuffix([]byte(s.Description), []byte(".")) && s.Description != "" {
			desc += "."
		}

		if s.Default != nil {
			if s.Default == "" {
				desc += " Defaults to `\"\"`."
			} else {
				desc += fmt.Sprintf(" Defaults to `%v`.", s.Default)
			}
		}

		if s.RequiredWith != nil && len(s.RequiredWith) > 0 && !contains(s.RequiredWith, consts.APIKeySchemaKey) {
			requiredWith := make([]string, len(s.RequiredWith))
			for i, c := range s.RequiredWith {
				requiredWith[i] = fmt.Sprintf("`%s`", c)
			}
			desc += fmt.Sprintf(" Required when using %s.", strings.Join(requiredWith, ", "))
		}

		if s.ConflictsWith != nil && len(s.ConflictsWith) > 0 && !contains(s.ConflictsWith, consts.APITokenSchemaKey) {
			conflicts := make([]string, len(s.ConflictsWith))
			for i, c := range s.ConflictsWith {
				conflicts[i] = fmt.Sprintf("`%s`", c)
			}
			desc += fmt.Sprintf(" Conflicts with %s.", strings.Join(conflicts, ", "))
		}

		if s.ExactlyOneOf != nil && len(s.ExactlyOneOf) > 0 && (!contains(s.ExactlyOneOf, consts.APIKeySchemaKey) || !contains(s.ExactlyOneOf, consts.APITokenSchemaKey) || !contains(s.ExactlyOneOf, consts.APIUserServiceKeySchemaKey)) {
			exactlyOneOfs := make([]string, len(s.ExactlyOneOf))
			for i, c := range s.ExactlyOneOf {
				exactlyOneOfs[i] = fmt.Sprintf("`%s`", c)
			}
			desc += fmt.Sprintf(" Must provide only one of %s.", strings.Join(exactlyOneOfs, ", "))
		}

		if s.AtLeastOneOf != nil && len(s.AtLeastOneOf) > 0 {
			atLeastOneOfs := make([]string, len(s.AtLeastOneOf))
			for i, c := range s.AtLeastOneOf {
				atLeastOneOfs[i] = fmt.Sprintf("`%s`", c)
			}
			desc += fmt.Sprintf(" Must provide at least one of %s.", strings.Join(atLeastOneOfs, ", "))
		}

		if s.ForceNew {
			desc += " **Modifying this attribute will force creation of a new resource.**"
		}

		return strings.TrimSpace(desc)
	}
}

func New(version string) func() *schema.Provider {
	return func() *schema.Provider {
		p := &schema.Provider{
			Schema: map[string]*schema.Schema{
				consts.EmailSchemaKey: {
					Type:          schema.TypeString,
					Optional:      true,
					Description:   fmt.Sprintf("A registered Cloudflare email address. Alternatively, can be configured using the `%s` environment variable. Required when using `api_key`. Conflicts with `api_token`.", consts.EmailEnvVarKey),
					ConflictsWith: []string{consts.APITokenSchemaKey},
					RequiredWith:  []string{consts.APIKeySchemaKey},
				},

				consts.APIKeySchemaKey: {
					Type:         schema.TypeString,
					Optional:     true,
					Description:  fmt.Sprintf("The API key for operations. Alternatively, can be configured using the `%s` environment variable. API keys are [now considered legacy by Cloudflare](https://developers.cloudflare.com/api/keys/#limitations), API tokens should be used instead. Must provide only one of `api_key`, `api_token`, `api_user_service_key`.", consts.APIKeyEnvVarKey),
					ValidateFunc: validation.StringMatch(regexp.MustCompile("[0-9a-f]{37}"), "API key must be 37 characters long and only contain characters 0-9 and a-f (all lowercased)"),
				},

				consts.APITokenSchemaKey: {
					Type:         schema.TypeString,
					Optional:     true,
					Description:  fmt.Sprintf("The API Token for operations. Alternatively, can be configured using the `%s` environment variable. Must provide only one of `api_key`, `api_token`, `api_user_service_key`.", consts.APITokenEnvVarKey),
					ValidateFunc: validation.StringMatch(regexp.MustCompile("[A-Za-z0-9-_]{40}"), "API tokens must be 40 characters long and only contain characters a-z, A-Z, 0-9, hyphens and underscores"),
				},

				consts.APIUserServiceKeySchemaKey: {
					Type:        schema.TypeString,
					Optional:    true,
					Description: fmt.Sprintf("A special Cloudflare API key good for a restricted set of endpoints. Alternatively, can be configured using the `%s` environment variable. Must provide only one of `api_key`, `api_token`, `api_user_service_key`.", consts.APIUserServiceKeyEnvVarKey),
				},

				consts.RPSSchemaKey: {
					Type:        schema.TypeInt,
					Optional:    true,
					Description: fmt.Sprintf("RPS limit to apply when making calls to the API. Alternatively, can be configured using the `%s` environment variable.", consts.RPSEnvVarKey),
				},

				consts.RetriesSchemaKey: {
					Type:        schema.TypeInt,
					Optional:    true,
					Description: fmt.Sprintf("Maximum number of retries to perform when an API request fails. Alternatively, can be configured using the `%s` environment variable.", consts.RetriesEnvVarKey),
				},

				consts.MinimumBackoffSchemaKey: {
					Type:        schema.TypeInt,
					Optional:    true,
					Description: fmt.Sprintf("Minimum backoff period in seconds after failed API calls. Alternatively, can be configured using the `%s` environment variable.", consts.MinimumBackoffEnvVar),
				},

				consts.MaximumBackoffSchemaKey: {
					Type:        schema.TypeInt,
					Optional:    true,
					Description: fmt.Sprintf("Maximum backoff period in seconds after failed API calls. Alternatively, can be configured using the `%s` environment variable.", consts.MaximumBackoffEnvVarKey),
				},

				consts.APIClientLoggingSchemaKey: {
					Type:        schema.TypeBool,
					Optional:    true,
					Description: fmt.Sprintf("Whether to print logs from the API client (using the default log library logger). Alternatively, can be configured using the `%s` environment variable.", consts.APIClientLoggingEnvVarKey),
				},

				consts.AccountIDSchemaKey: {
					Type:        schema.TypeString,
					Optional:    true,
					Description: fmt.Sprintf("Configure API client to always use a specific account. Alternatively, can be configured using the `%s` environment variable.", consts.AccountIDEnvVarKey),
					Deprecated:  "Use resource specific `account_id` attributes instead.",
				},

				consts.APIHostnameSchemaKey: {
					Type:        schema.TypeString,
					Optional:    true,
					Description: fmt.Sprintf("Configure the hostname used by the API client. Alternatively, can be configured using the `%s` environment variable.", consts.APIHostnameEnvVarKey),
				},

				consts.APIBasePathSchemaKey: {
					Type:        schema.TypeString,
					Optional:    true,
					Description: fmt.Sprintf("Configure the base path used by the API client. Alternatively, can be configured using the `%s` environment variable.", consts.APIBasePathEnvVarKey),
				},
			},

			DataSourcesMap: map[string]*schema.Resource{
				"cloudflare_access_identity_provider":    dataSourceCloudflareAccessIdentityProvider(),
				"cloudflare_account_roles":               dataSourceCloudflareAccountRoles(),
				"cloudflare_accounts":                    dataSourceCloudflareAccounts(),
				"cloudflare_api_token_permission_groups": dataSourceCloudflareApiTokenPermissionGroups(),
				"cloudflare_devices":                     dataSourceCloudflareDevices(),
				"cloudflare_ip_ranges":                   dataSourceCloudflareIPRanges(),
				"cloudflare_load_balancer_pools":         dataSourceCloudflareLoadBalancerPools(),
				"cloudflare_origin_ca_root_certificate":  dataSourceCloudflareOriginCARootCertificate(),
				"cloudflare_record":                      dataSourceCloudflareRecord(),
				"cloudflare_waf_groups":                  dataSourceCloudflareWAFGroups(),
				"cloudflare_waf_packages":                dataSourceCloudflareWAFPackages(),
				"cloudflare_waf_rules":                   dataSourceCloudflareWAFRules(),
				"cloudflare_zone_dnssec":                 dataSourceCloudflareZoneDNSSEC(),
				"cloudflare_zone":                        dataSourceCloudflareZone(),
				"cloudflare_zones":                       dataSourceCloudflareZones(),
			},

			ResourcesMap: map[string]*schema.Resource{
				"cloudflare_access_application":                     resourceCloudflareAccessApplication(),
				"cloudflare_access_bookmark":                        resourceCloudflareAccessBookmark(),
				"cloudflare_access_ca_certificate":                  resourceCloudflareAccessCACertificate(),
				"cloudflare_access_group":                           resourceCloudflareAccessGroup(),
				"cloudflare_access_identity_provider":               resourceCloudflareAccessIdentityProvider(),
				"cloudflare_access_keys_configuration":              resourceCloudflareAccessKeysConfiguration(),
				"cloudflare_access_mutual_tls_certificate":          resourceCloudflareAccessMutualTLSCertificate(),
				"cloudflare_access_organization":                    resourceCloudflareAccessOrganization(),
				"cloudflare_access_policy":                          resourceCloudflareAccessPolicy(),
				"cloudflare_access_rule":                            resourceCloudflareAccessRule(),
				"cloudflare_access_service_token":                   resourceCloudflareAccessServiceToken(),
				"cloudflare_account_member":                         resourceCloudflareAccountMember(),
				"cloudflare_account":                                resourceCloudflareAccount(),
				"cloudflare_api_shield":                             resourceCloudflareAPIShield(),
				"cloudflare_api_token":                              resourceCloudflareApiToken(),
				"cloudflare_argo_tunnel":                            resourceCloudflareArgoTunnel(),
				"cloudflare_argo":                                   resourceCloudflareArgo(),
				"cloudflare_authenticated_origin_pulls_certificate": resourceCloudflareAuthenticatedOriginPullsCertificate(),
				"cloudflare_authenticated_origin_pulls":             resourceCloudflareAuthenticatedOriginPulls(),
				"cloudflare_byo_ip_prefix":                          resourceCloudflareBYOIPPrefix(),
				"cloudflare_certificate_pack":                       resourceCloudflareCertificatePack(),
				"cloudflare_custom_hostname_fallback_origin":        resourceCloudflareCustomHostnameFallbackOrigin(),
				"cloudflare_custom_hostname":                        resourceCloudflareCustomHostname(),
				"cloudflare_custom_pages":                           resourceCloudflareCustomPages(),
				"cloudflare_custom_ssl":                             resourceCloudflareCustomSsl(),
				"cloudflare_device_settings_policy":                 resourceCloudflareDeviceSettingsPolicy(),
				"cloudflare_device_policy_certificates":             resourceCloudflareDevicePolicyCertificates(),
				"cloudflare_device_posture_integration":             resourceCloudflareDevicePostureIntegration(),
				"cloudflare_device_posture_rule":                    resourceCloudflareDevicePostureRule(),
				"cloudflare_device_managed_networks":                resourceCloudflareDeviceManagedNetworks(),
				"cloudflare_dlp_profile":                            resourceCloudflareDLPProfile(),
				"cloudflare_email_routing_address":                  resourceCloudflareEmailRoutingAddress(),
				"cloudflare_email_routing_catch_all":                resourceCloudflareEmailRoutingCatchAll(),
				"cloudflare_email_routing_rule":                     resourceCloudflareEmailRoutingRule(),
				"cloudflare_email_routing_settings":                 resourceCloudflareEmailRoutingSettings(),
				"cloudflare_fallback_domain":                        resourceCloudflareFallbackDomain(),
				"cloudflare_filter":                                 resourceCloudflareFilter(),
				"cloudflare_firewall_rule":                          resourceCloudflareFirewallRule(),
				"cloudflare_gre_tunnel":                             resourceCloudflareGRETunnel(),
				"cloudflare_healthcheck":                            resourceCloudflareHealthcheck(),
				"cloudflare_ip_list":                                resourceCloudflareIPList(),
				"cloudflare_ipsec_tunnel":                           resourceCloudflareIPsecTunnel(),
				"cloudflare_list":                                   resourceCloudflareList(),
				"cloudflare_load_balancer_monitor":                  resourceCloudflareLoadBalancerMonitor(),
				"cloudflare_load_balancer_pool":                     resourceCloudflareLoadBalancerPool(),
				"cloudflare_load_balancer":                          resourceCloudflareLoadBalancer(),
				"cloudflare_logpull_retention":                      resourceCloudflareLogpullRetention(),
				"cloudflare_logpush_job":                            resourceCloudflareLogpushJob(),
				"cloudflare_logpush_ownership_challenge":            resourceCloudflareLogpushOwnershipChallenge(),
				"cloudflare_magic_firewall_ruleset":                 resourceCloudflareMagicFirewallRuleset(),
				"cloudflare_managed_headers":                        resourceCloudflareManagedHeaders(),
				"cloudflare_notification_policy_webhooks":           resourceCloudflareNotificationPolicyWebhook(),
				"cloudflare_notification_policy":                    resourceCloudflareNotificationPolicy(),
				"cloudflare_origin_ca_certificate":                  resourceCloudflareOriginCACertificate(),
				"cloudflare_page_rule":                              resourceCloudflarePageRule(),
				"cloudflare_pages_domain":                           resourceCloudflarePagesDomain(),
				"cloudflare_pages_project":                          resourceCloudflarePagesProject(),
				"cloudflare_rate_limit":                             resourceCloudflareRateLimit(),
				"cloudflare_record":                                 resourceCloudflareRecord(),
				"cloudflare_ruleset":                                resourceCloudflareRuleset(),
				"cloudflare_spectrum_application":                   resourceCloudflareSpectrumApplication(),
				"cloudflare_split_tunnel":                           resourceCloudflareSplitTunnel(),
				"cloudflare_static_route":                           resourceCloudflareStaticRoute(),
				"cloudflare_teams_account":                          resourceCloudflareTeamsAccount(),
				"cloudflare_teams_list":                             resourceCloudflareTeamsList(),
				"cloudflare_teams_location":                         resourceCloudflareTeamsLocation(),
				"cloudflare_teams_proxy_endpoint":                   resourceCloudflareTeamsProxyEndpoint(),
				"cloudflare_tiered_cache":                           resourceCloudflareTieredCache(),
				"cloudflare_tunnel_config":                          resourceCloudflareTunnelConfig(),
				"cloudflare_teams_rule":                             resourceCloudflareTeamsRule(),
				"cloudflare_total_tls":                              resourceCloudflareTotalTLS(),
				"cloudflare_tunnel_route":                           resourceCloudflareTunnelRoute(),
				"cloudflare_tunnel_virtual_network":                 resourceCloudflareTunnelVirtualNetwork(),
				"cloudflare_url_normalization_settings":             resourceCloudflareURLNormalizationSettings(),
				"cloudflare_user_agent_blocking_rule":               resourceCloudflareUserAgentBlockingRules(),
				"cloudflare_waf_group":                              resourceCloudflareWAFGroup(),
				"cloudflare_waf_override":                           resourceCloudflareWAFOverride(),
				"cloudflare_waf_package":                            resourceCloudflareWAFPackage(),
				"cloudflare_waf_rule":                               resourceCloudflareWAFRule(),
				"cloudflare_waiting_room_event":                     resourceCloudflareWaitingRoomEvent(),
				"cloudflare_waiting_room_rules":                     resourceCloudflareWaitingRoomRules(),
				"cloudflare_waiting_room":                           resourceCloudflareWaitingRoom(),
				"cloudflare_web3_hostname":                          resourceCloudflareWeb3Hostname(),
				"cloudflare_worker_cron_trigger":                    resourceCloudflareWorkerCronTrigger(),
				"cloudflare_worker_route":                           resourceCloudflareWorkerRoute(),
				"cloudflare_worker_script":                          resourceCloudflareWorkerScript(),
				"cloudflare_workers_kv_namespace":                   resourceCloudflareWorkersKVNamespace(),
				"cloudflare_workers_kv":                             resourceCloudflareWorkerKV(),
				"cloudflare_zone_cache_variants":                    resourceCloudflareZoneCacheVariants(),
				"cloudflare_zone_dnssec":                            resourceCloudflareZoneDNSSEC(),
				"cloudflare_zone_lockdown":                          resourceCloudflareZoneLockdown(),
				"cloudflare_zone_settings_override":                 resourceCloudflareZoneSettingsOverride(),
				"cloudflare_zone":                                   resourceCloudflareZone(),
			},
		}

		p.ConfigureContextFunc = configure(version, p)

		return p
	}
}

func configure(version string, p *schema.Provider) func(context.Context, *schema.ResourceData) (interface{}, diag.Diagnostics) {
	return func(ctx context.Context, d *schema.ResourceData) (interface{}, diag.Diagnostics) {
		var (
			diags diag.Diagnostics

			email             string
			apiKey            string
			apiToken          string
			apiUserServiceKey string
			rps               int64
			retries           int64
			minBackOff        int64
			maxBackOff        int64
			accountID         string
			baseHostname      string
			basePath          string
		)

		if d.Get(consts.APIHostnameSchemaKey).(string) != "" {
			baseHostname = d.Get(consts.APIHostnameSchemaKey).(string)
		} else {
			baseHostname = utils.GetDefaultFromEnv(consts.APIHostnameEnvVarKey, consts.APIHostnameDefault)
		}

		if d.Get(consts.APIBasePathSchemaKey).(string) != "" {
			basePath = d.Get(consts.APIBasePathSchemaKey).(string)
		} else {
			basePath = utils.GetDefaultFromEnv(consts.APIBasePathEnvVarKey, consts.APIBasePathDefault)
		}
		baseURL := cloudflare.BaseURL(fmt.Sprintf("https://%s%s", baseHostname, basePath))

		if _, ok := d.GetOk(consts.RPSSchemaKey); ok {
			rps = int64(d.Get(consts.RPSSchemaKey).(int))
		} else {
			i, _ := strconv.ParseInt(utils.GetDefaultFromEnv(consts.RPSEnvVarKey, consts.RPSDefault), 10, 64)
			rps = i
		}
		limitOpt := cloudflare.UsingRateLimit(float64(rps))

		if _, ok := d.GetOk(consts.RetriesSchemaKey); ok {
			retries = int64(d.Get(consts.RetriesSchemaKey).(int))
		} else {
			i, _ := strconv.ParseInt(utils.GetDefaultFromEnv(consts.RetriesEnvVarKey, consts.RetriesDefault), 10, 64)
			retries = i
		}

		if _, ok := d.GetOk(consts.MinimumBackoffSchemaKey); ok {
			minBackOff = int64(d.Get(consts.MinimumBackoffSchemaKey).(int))
		} else {
			i, _ := strconv.ParseInt(utils.GetDefaultFromEnv(consts.MinimumBackoffEnvVar, consts.MinimumBackoffDefault), 10, 64)
			minBackOff = i
		}

		if _, ok := d.GetOk(consts.MaximumBackoffSchemaKey); ok {
			maxBackOff = int64(d.Get(consts.MaximumBackoffSchemaKey).(int))
		} else {
			i, _ := strconv.ParseInt(utils.GetDefaultFromEnv(consts.MaximumBackoffEnvVarKey, consts.MaximumBackoffDefault), 10, 64)
			maxBackOff = i
		}

		if retries > strconv.IntSize {
			diags = append(diags, diag.Diagnostic{
				Severity: diag.Error,
				Summary:  fmt.Sprintf("retries value of %d is too large, try a smaller value.", retries),
			})

			return nil, diags
		}

		if minBackOff > strconv.IntSize {
			diags = append(diags, diag.Diagnostic{
				Severity: diag.Error,
				Summary:  fmt.Sprintf("min_backoff value of %d is too large, try a smaller value.", minBackOff),
			})

			return nil, diags
		}

		if maxBackOff > strconv.IntSize {
			diags = append(diags, diag.Diagnostic{
				Severity: diag.Error,
				Summary:  fmt.Sprintf("max_backoff value of %d is too large, try a smaller value.", maxBackOff),
			})

			return nil, diags
		}

		retryOpt := cloudflare.UsingRetryPolicy(int(retries), int(minBackOff), int(maxBackOff))
		options := []cloudflare.Option{limitOpt, retryOpt, baseURL}

		options = append(options, cloudflare.Debug(logging.IsDebugOrHigher()))

		ua := fmt.Sprintf(consts.UserAgentDefault, p.TerraformVersion, meta.SDKVersionString(), version)
		options = append(options, cloudflare.UserAgent(ua))

		config := Config{Options: options}

		if v, ok := d.GetOk(consts.APITokenSchemaKey); ok {
			apiToken = v.(string)
		} else {
			apiToken = utils.GetDefaultFromEnv(consts.APITokenEnvVarKey, "")
		}

		if apiToken != "" {
			config.APIToken = apiToken
		}

		if v, ok := d.GetOk(consts.APIKeySchemaKey); ok {
			apiKey = v.(string)
		} else {
			apiKey = utils.GetDefaultFromEnv(consts.APIKeyEnvVarKey, "")
		}

		if apiKey != "" {
			config.APIKey = apiKey

			if v, ok := d.GetOk(consts.EmailSchemaKey); ok {
				email = v.(string)
			} else {
				email = utils.GetDefaultFromEnv(consts.EmailEnvVarKey, "")
			}

			if email == "" {
				diags = append(diags, diag.Diagnostic{
					Severity: diag.Error,
					Summary:  fmt.Sprintf("%q is not set correctly", consts.EmailSchemaKey),
				})

				return nil, diags
			}

			if email != "" {
				config.Email = email
			}
		}

		if v, ok := d.GetOk(consts.APIUserServiceKeySchemaKey); ok {
			apiUserServiceKey = v.(string)
		} else {
			apiUserServiceKey = utils.GetDefaultFromEnv(consts.APIUserServiceKeyEnvVarKey, "")
		}

		if apiUserServiceKey != "" {
			config.APIUserServiceKey = apiUserServiceKey
		}

		if apiKey == "" && apiToken == "" && apiUserServiceKey == "" {
			diags = append(diags, diag.Diagnostic{
				Severity: diag.Error,
				Summary:  fmt.Sprintf("must provide one of %q, %q or %q.", consts.APIKeySchemaKey, consts.APITokenSchemaKey, consts.APIUserServiceKeySchemaKey),
			})
			return nil, diags
		}

		if v, ok := d.GetOk(consts.AccountIDSchemaKey); ok {
			accountID = v.(string)
		} else {
			accountID = utils.GetDefaultFromEnv(consts.AccountIDEnvVarKey, "")
		}

		if accountID != "" {
			tflog.Info(ctx, fmt.Sprintf("using specified account id %s in Cloudflare provider", accountID))
			options = append(options, cloudflare.UsingAccount(accountID))
		}

		config.Options = options
		client, err := config.Client(ctx)
		if err != nil {
			return nil, diag.FromErr(err)
		}

		return client, nil
	}
}
