package tencentcloud

import (
	"context"
	"time"

	"github.com/cenkalti/backoff/v4"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"

	tencentCloudClbClient "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/clb/v20180317"
)

var (
	_ datasource.DataSource              = &clbInstancesDataSource{}
	_ datasource.DataSourceWithConfigure = &clbInstancesDataSource{}
)

func NewClbInstancesDataSource() datasource.DataSource {
	return &clbInstancesDataSource{}
}

type clbInstancesDataSource struct {
	client *tencentCloudClbClient.Client
}

type clbInstancesDataSourceModel struct {
	ClientConfig  *clientConfigWithZone     `tfsdk:"client_config"`
	Id            types.String              `tfsdk:"id"`
	Name          types.String              `tfsdk:"name"`
	Tags          types.Map                 `tfsdk:"tags"`
	LoadBalancers []*clbLoadBalancersDetail `tfsdk:"load_balancers"`
}

type clbLoadBalancersDetail struct {
	Id   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
	Tags types.Map    `tfsdk:"tags"`
}

func (d *clbInstancesDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_clb_instances"
}

func (d *clbInstancesDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "This data source provides the Cloud Load Balancers of the current Tencent Cloud user.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "ID of Cloud Load Balancers to query",
				Optional:    true,
			},
			"name": schema.StringAttribute{
				Description: "Name of Cloud Load Balancers to query",
				Optional:    true,
			},
			"tags": schema.MapAttribute{
				Description: "Tags of Cloud Load Balancers to query",
				ElementType: types.StringType,
				Optional:    true,
			},
			"load_balancers": schema.ListNestedAttribute{
				Description: "Result list of Cloud Load Balancers queried",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Description: "The ID of the Cloud Load Balancer",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "The Name of the Cloud Load Balancer",
							Computed:    true,
						},
						"tags": schema.MapAttribute{
							Description: "The Tags of the Cloud Load Balancer",
							ElementType: types.StringType,
							Computed:    true,
						},
					},
				},
			},
		},
		Blocks: map[string]schema.Block{
			"client_config": schema.SingleNestedBlock{
				Description: "Config to override default client created in Provider. " +
					"This block will not be recorded in state file.",
				Attributes: map[string]schema.Attribute{
					"region": schema.StringAttribute{
						Description: "The region of the CLBs. Default to use region " +
							"configured in the provider.",
						Optional: true,
					},
					"zone": schema.StringAttribute{
						Description: "The zone of TencentCloud CLBs.",
						Optional:    true,
					},
					"secret_id": schema.StringAttribute{
						Description: "The secret id that have permissions to list " +
							"CLBs. Default to use secret id configured in the provider.",
						Optional: true,
					},
					"secret_key": schema.StringAttribute{
						Description: "The secret key that have permissions to list " +
							"CLBs. Default to use secret key configured in the provider.",
						Optional: true,
					},
				},
			},
		},
	}
}

func (d *clbInstancesDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	d.client = req.ProviderData.(tencentCloudClients).clbClient
}

func (d *clbInstancesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var plan, state *clbInstancesDataSourceModel
	getPlanDiags := req.Config.Get(ctx, &plan)
	resp.Diagnostics.Append(getPlanDiags...)
	if getPlanDiags.HasError() {
		return
	}

	if plan.ClientConfig == nil {
		plan.ClientConfig = &clientConfigWithZone{}
	}

	initClient, clientConfig := initNewClient(&d.client.Client, plan.ClientConfig.getClientConfig())
	if initClient {
		var err error
		d.client, err = tencentCloudClbClient.NewClient(clientConfig.credential, clientConfig.region, profile.NewClientProfile())
		if err != nil {
			resp.Diagnostics.AddError(
				"Unable to Reinitialize Tencent Cloud Load Balancers API Client",
				"An unexpected error occurred when creating the Tencent Cloud Load Balancers API client. "+
					"If the error is not clear, please contact the provider developers.\n\n"+
					"Tencent Cloud Load Balancers Client Error: "+err.Error(),
			)
			return
		}
	}

	state = &clbInstancesDataSourceModel{
		LoadBalancers: []*clbLoadBalancersDetail{},
	}
	state.Id = plan.Id
	state.Name = plan.Name
	state.Tags = plan.Tags

	// Create Describe Load Balancers Request
	describeLoadBalancersRequest := tencentCloudClbClient.NewDescribeLoadBalancersRequest()

	if !(plan.Name.IsUnknown() || plan.Name.IsNull()) {
		describeLoadBalancersRequest.LoadBalancerName = common.StringPtr(plan.Name.ValueString())
	}

	if !(plan.Id.IsUnknown() || plan.Id.IsNull()) {
		describeLoadBalancersRequest.LoadBalancerIds = []*string{common.StringPtr(state.Id.ValueString())}
	}

	if !(plan.Tags.IsUnknown() || plan.Tags.IsNull()) {
		inputTags := make(map[string]string)

		// Convert from Terraform map type to Go map type
		convertTagsDiags := plan.Tags.ElementsAs(ctx, &inputTags, false)
		resp.Diagnostics.Append(convertTagsDiags...)
		if resp.Diagnostics.HasError() {
			return
		}

		// Get all filter tags from Plan and convert them into Tencent Cloud Filter type
		filterList := []*tencentCloudClbClient.Filter{}
		for inputKey, inputValue := range inputTags {
			filterDetail := &tencentCloudClbClient.Filter{
				Name:   common.StringPtr("tag:" + inputKey),
				Values: common.StringPtrs([]string{inputValue}),
			}
			filterList = append(filterList, filterDetail)
		}

		describeLoadBalancersRequest.Filters = filterList
	}

	if plan.ClientConfig.Zone.ValueString() != "" {
		zoneId, err := getZoneId(&d.client.Client, plan.ClientConfig.Zone.ValueString())
		if err != nil {
			resp.Diagnostics.AddError(
				"[API ERROR] Failed to Get Zone ID",
				"This is an error in provider. Please contact provider developer\n\n"+
					"Error: "+err.Error(),
			)
			return
		}
		describeLoadBalancersRequest.MasterZone = common.StringPtr(zoneId)
	}

	describeLb := func() error {
		// Describe Load Balancers
		describeLoadBalancersResponse, err := d.client.DescribeLoadBalancers(describeLoadBalancersRequest)
		if err != nil {
			if terr, ok := err.(*errors.TencentCloudSDKError); ok {
				if isRetryableErrCode(terr.GetCode()) {
					return err
				} else {
					return backoff.Permanent(err)
				}
			} else {
				return err
			}
		}

		// Store Load Balancers into Terraform state
		for _, lbSet := range describeLoadBalancersResponse.Response.LoadBalancerSet {
			if len(lbSet.Tags) < 1 {
				clbDetail := &clbLoadBalancersDetail{
					Id:   types.StringValue(*lbSet.LoadBalancerId),
					Name: types.StringValue(*lbSet.LoadBalancerName),
					Tags: types.MapNull(types.StringType),
				}
				state.LoadBalancers = append(state.LoadBalancers, clbDetail)
				continue
			} else {
				// Convert API output Tags to Go map
				clbTagMap := make(map[string]attr.Value)
				count := len(lbSet.Tags)
				for i := 0; i < count; i++ {
					clbTagMap[*lbSet.Tags[i].TagKey] = types.StringValue(*lbSet.Tags[i].TagValue)
				}

				clbDetail := &clbLoadBalancersDetail{
					Id:   types.StringValue(*lbSet.LoadBalancerId),
					Name: types.StringValue(*lbSet.LoadBalancerName),
					Tags: types.MapValueMust(types.StringType, clbTagMap),
				}
				state.LoadBalancers = append(state.LoadBalancers, clbDetail)
			}
		}
		return nil
	}

	reconnectBackoff := backoff.NewExponentialBackOff()
	reconnectBackoff.MaxElapsedTime = 30 * time.Second

	err := backoff.Retry(describeLb, reconnectBackoff)
	if err != nil {
		resp.Diagnostics.AddError(
			"[API ERROR] Failed to Describe Load Balancers",
			err.Error(),
		)
		return
	}

	setStateDiags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(setStateDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
}
