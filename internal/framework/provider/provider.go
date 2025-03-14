package provider

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"testing"

	"github.com/cloudflare/cloudflare-go"
	"github.com/cloudflare/terraform-provider-cloudflare/internal/consts"
	"github.com/cloudflare/terraform-provider-cloudflare/internal/utils"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/logging"
	"github.com/hashicorp/terraform-plugin-sdk/v2/meta"
)

// Ensure CloudflareProvider satisfies various provider interfaces.
var _ provider.Provider = &CloudflareProvider{}

// CloudflareProvider defines the provider implementation.
type CloudflareProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

// CloudflareProviderModel describes the provider data model.
type CloudflareProviderModel struct {
	APIKey            types.String `tfsdk:"api_key"`
	APIUserServiceKey types.String `tfsdk:"api_user_service_key"`
	Email             types.String `tfsdk:"email"`
	MinBackOff        types.Int64  `tfsdk:"min_backoff"`
	RPS               types.Int64  `tfsdk:"rps"`
	AccountID         types.String `tfsdk:"account_id"`
	APIBasePath       types.String `tfsdk:"api_base_path"`
	APIToken          types.String `tfsdk:"api_token"`
	Retries           types.Int64  `tfsdk:"retries"`
	MaxBackoff        types.Int64  `tfsdk:"max_backoff"`
	APIClientLogging  types.Bool   `tfsdk:"api_client_logging"`
	APIHostname       types.String `tfsdk:"api_hostname"`
}

func (p *CloudflareProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "cloudflare"
	resp.Version = p.version
}

func (p *CloudflareProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			consts.EmailSchemaKey: schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: fmt.Sprintf("A registered Cloudflare email address. Alternatively, can be configured using the `%s` environment variable. Required when using `api_key`. Conflicts with `api_token`.", consts.EmailEnvVarKey),
				Validators:          []validator.String{},
			},

			consts.APIKeySchemaKey: schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: fmt.Sprintf("The API key for operations. Alternatively, can be configured using the `%s` environment variable. API keys are [now considered legacy by Cloudflare](https://developers.cloudflare.com/api/keys/#limitations), API tokens should be used instead. Must provide only one of `api_key`, `api_token`, `api_user_service_key`.", consts.APIKeyEnvVarKey),
				Validators: []validator.String{
					stringvalidator.RegexMatches(
						regexp.MustCompile(`[0-9a-f]{37}`),
						"API key must be 37 characters long and only contain characters 0-9 and a-f (all lowercased)",
					),
					stringvalidator.AlsoRequires(path.Expressions{
						path.MatchRoot(consts.EmailSchemaKey),
					}...),
					stringvalidator.ExactlyOneOf(path.Expressions{
						path.MatchRoot(consts.APIKeySchemaKey),
						path.MatchRoot(consts.APITokenSchemaKey),
						path.MatchRoot(consts.APIUserServiceKeySchemaKey),
					}...),
				},
			},

			consts.APITokenSchemaKey: schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: fmt.Sprintf("The API Token for operations. Alternatively, can be configured using the `%s` environment variable. Must provide only one of `api_key`, `api_token`, `api_user_service_key`.", consts.APITokenEnvVarKey),
				Validators: []validator.String{
					stringvalidator.RegexMatches(
						regexp.MustCompile(`[A-Za-z0-9-_]{40}`),
						"API tokens must be 40 characters long and only contain characters a-z, A-Z, 0-9, hyphens and underscores",
					),
					stringvalidator.ExactlyOneOf(path.Expressions{
						path.MatchRoot(consts.APIKeySchemaKey),
						path.MatchRoot(consts.APITokenSchemaKey),
						path.MatchRoot(consts.APIUserServiceKeySchemaKey),
					}...),
				},
			},

			consts.APIUserServiceKeySchemaKey: schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: fmt.Sprintf("A special Cloudflare API key good for a restricted set of endpoints. Alternatively, can be configured using the `%s` environment variable. Must provide only one of `api_key`, `api_token`, `api_user_service_key`.", consts.APIUserServiceKeyEnvVarKey),
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(path.Expressions{
						path.MatchRoot(consts.APIKeySchemaKey),
						path.MatchRoot(consts.APITokenSchemaKey),
						path.MatchRoot(consts.APIUserServiceKeySchemaKey),
					}...),
				},
			},

			consts.RPSSchemaKey: schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: fmt.Sprintf("RPS limit to apply when making calls to the API. Alternatively, can be configured using the `%s` environment variable.", consts.RPSEnvVarKey),
			},

			consts.RetriesSchemaKey: schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: fmt.Sprintf("Maximum number of retries to perform when an API request fails. Alternatively, can be configured using the `%s` environment variable.", consts.RetriesEnvVarKey),
			},

			consts.MinimumBackoffSchemaKey: schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: fmt.Sprintf("Minimum backoff period in seconds after failed API calls. Alternatively, can be configured using the `%s` environment variable.", consts.MinimumBackoffEnvVar),
			},

			consts.MaximumBackoffSchemaKey: schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: fmt.Sprintf("Maximum backoff period in seconds after failed API calls. Alternatively, can be configured using the `%s` environment variable.", consts.MaximumBackoffEnvVarKey),
			},

			consts.APIClientLoggingSchemaKey: schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: fmt.Sprintf("Whether to print logs from the API client (using the default log library logger). Alternatively, can be configured using the `%s` environment variable.", consts.APIClientLoggingEnvVarKey),
			},

			consts.AccountIDSchemaKey: schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: fmt.Sprintf("Configure API client to always use a specific account. Alternatively, can be configured using the `%s` environment variable.", consts.AccountIDEnvVarKey),
				DeprecationMessage:  "Use resource specific `account_id` attributes instead.",
			},

			consts.APIHostnameSchemaKey: schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: fmt.Sprintf("Configure the hostname used by the API client. Alternatively, can be configured using the `%s` environment variable.", consts.APIHostnameEnvVarKey),
			},

			consts.APIBasePathSchemaKey: schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: fmt.Sprintf("Configure the base path used by the API client. Alternatively, can be configured using the `%s` environment variable.", consts.APIBasePathEnvVarKey),
			},
		},
	}
}

func (p *CloudflareProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var (
		data CloudflareProviderModel

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

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.APIHostname.ValueString() != "" {
		baseHostname = data.APIHostname.ValueString()
	} else {
		baseHostname = utils.GetDefaultFromEnv(consts.APIHostnameEnvVarKey, consts.APIHostnameDefault)
	}

	if data.APIBasePath.ValueString() != "" {
		basePath = data.APIBasePath.ValueString()
	} else {
		basePath = utils.GetDefaultFromEnv(consts.APIBasePathEnvVarKey, consts.APIBasePathDefault)
	}
	baseURL := cloudflare.BaseURL(fmt.Sprintf("https://%s%s", baseHostname, basePath))

	if !data.RPS.IsNull() {
		rps = int64(data.RPS.ValueInt64())
	} else {
		i, _ := strconv.ParseInt(utils.GetDefaultFromEnv(consts.RPSEnvVarKey, consts.RPSDefault), 10, 64)
		rps = i
	}
	limitOpt := cloudflare.UsingRateLimit(float64(rps))

	if !data.Retries.IsNull() {
		retries = int64(data.Retries.ValueInt64())
	} else {
		i, _ := strconv.ParseInt(utils.GetDefaultFromEnv(consts.RetriesEnvVarKey, consts.RetriesDefault), 10, 64)
		retries = i
	}

	if !data.MinBackOff.IsNull() {
		minBackOff = int64(data.MaxBackoff.ValueInt64())
	} else {
		i, _ := strconv.ParseInt(utils.GetDefaultFromEnv(consts.MinimumBackoffEnvVar, consts.MinimumBackoffDefault), 10, 64)
		minBackOff = i
	}

	if !data.MinBackOff.IsNull() {
		maxBackOff = int64(data.MaxBackoff.ValueInt64())
	} else {
		i, _ := strconv.ParseInt(utils.GetDefaultFromEnv(consts.MaximumBackoffEnvVarKey, consts.MaximumBackoffDefault), 10, 64)
		maxBackOff = i
	}

	if retries > strconv.IntSize {
		resp.Diagnostics.AddError(
			fmt.Sprintf("retries value of %d is too large, try a smaller value.", retries),
			fmt.Sprintf("retries value of %d is too large, try a smaller value.", retries),
		)
		return
	}

	if minBackOff > strconv.IntSize {
		resp.Diagnostics.AddError(
			fmt.Sprintf("min_backoff value of %d is too large, try a smaller value.", minBackOff),
			fmt.Sprintf("min_backoff value of %d is too large, try a smaller value.", minBackOff),
		)
		return
	}

	if maxBackOff > strconv.IntSize {
		resp.Diagnostics.AddError(
			fmt.Sprintf("max_backoff value of %d is too large, try a smaller value.", maxBackOff),
			fmt.Sprintf("max_backoff value of %d is too large, try a smaller value.", maxBackOff),
		)
		return
	}

	retryOpt := cloudflare.UsingRetryPolicy(int(retries), int(minBackOff), int(maxBackOff))
	options := []cloudflare.Option{limitOpt, retryOpt, baseURL}

	options = append(options, cloudflare.Debug(logging.IsDebugOrHigher()))

	ua := fmt.Sprintf(consts.UserAgentDefault, req.TerraformVersion, meta.SDKVersionString(), p.version)
	options = append(options, cloudflare.UserAgent(ua))

	config := Config{Options: options}

	if !data.APIToken.IsNull() {
		apiToken = data.APIToken.ValueString()
	} else {
		apiToken = utils.GetDefaultFromEnv(consts.APITokenEnvVarKey, "")
	}

	if apiToken != "" {
		config.APIToken = apiToken
	}

	if !data.APIKey.IsNull() {
		apiKey = data.APIKey.ValueString()
	} else {
		apiKey = utils.GetDefaultFromEnv(consts.APIKeyEnvVarKey, "")
	}

	if apiKey != "" {
		config.APIKey = apiKey

		if !data.Email.IsNull() {
			email = data.Email.ValueString()
		} else {
			email = utils.GetDefaultFromEnv(consts.EmailEnvVarKey, "")
		}

		if email == "" {
			resp.Diagnostics.AddError(
				fmt.Sprintf("%q is not set correctly", consts.EmailSchemaKey),
				fmt.Sprintf("%q is required with %q and was not configured", consts.EmailSchemaKey, consts.APIKeySchemaKey))
			return
		}

		if email != "" {
			config.Email = email
		}
	}

	if !data.APIUserServiceKey.IsNull() {
		apiUserServiceKey = data.APIUserServiceKey.ValueString()
	} else {
		apiUserServiceKey = utils.GetDefaultFromEnv(consts.APIUserServiceKeyEnvVarKey, "")
	}

	if apiUserServiceKey != "" {
		config.APIUserServiceKey = apiUserServiceKey
	}

	if apiKey == "" && apiToken == "" && apiUserServiceKey == "" {
		resp.Diagnostics.AddError(
			fmt.Sprintf("must provide one of %q, %q or %q.", consts.APIKeySchemaKey, consts.APITokenSchemaKey, consts.APIUserServiceKeySchemaKey),
			fmt.Sprintf("must provide one of %q, %q or %q.", consts.APIKeySchemaKey, consts.APITokenSchemaKey, consts.APIUserServiceKeySchemaKey),
		)
		return
	}

	if !data.AccountID.IsNull() {
		accountID = data.AccountID.ValueString()
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
		resp.Diagnostics.AddError(
			"failed to initialize a new client",
			err.Error(),
		)
		return
	}

	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *CloudflareProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		// NewExampleResource,
	}
}

func (p *CloudflareProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		// NewExampleDataSource,
	}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &CloudflareProvider{
			version: version,
		}
	}
}

// testAccProtoV6ProviderFactories are used to instantiate a provider during
// acceptance testing. The factory function will be invoked for every Terraform
// CLI command executed to create a provider server to which the CLI can
// reattach.
var TestAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"scaffolding": providerserver.NewProtocol6WithError(New("test")()),
}

func TestAccPreCheck(t *testing.T) {
	// You can add code here to run prior to any test case execution, for example assertions
	// about the appropriate environment variables being set are common to see in a pre-check
	// function.
}
