package irtranslator

import (
	"context"
	"fmt"
	"sort"

	"google.golang.org/protobuf/types/known/anypb"

	envoy_config_core_v3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoy_config_listener_v3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	luav3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/lua/v3"
	routerv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	envoyhttp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	envoytcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	envoyauth "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/wrapperspb"

	envoy_tls_inspector "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/tls_inspector/v3"
	"github.com/solo-io/go-utils/contextutils"
	"google.golang.org/protobuf/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/ir"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/plugins"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/reports"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/utils"
)

const (
	DefaultHttpStatPrefix  = "http"
	UpstreamCodeFilterName = "envoy.filters.http.upstream_codec"
)

type filterChainTranslator struct {
	listener        ir.ListenerIR
	gateway         ir.GatewayIR
	routeConfigName string

	PluginPass TranslationPassPlugins
}

func computeListenerAddress(bindAddress string, port uint32, reporter reports.GatewayReporter) *envoy_config_core_v3.Address {
	_, isIpv4Address, err := utils.IsIpv4Address(bindAddress)
	if err != nil {
		// TODO: return error ????
		reporter.SetCondition(reports.GatewayCondition{
			Type:    gwv1.GatewayConditionProgrammed,
			Reason:  gwv1.GatewayReasonInvalid,
			Status:  metav1.ConditionFalse,
			Message: "Error processing listener: " + err.Error(),
		})
	}

	return &envoy_config_core_v3.Address{
		Address: &envoy_config_core_v3.Address_SocketAddress{
			SocketAddress: &envoy_config_core_v3.SocketAddress{
				Protocol: envoy_config_core_v3.SocketAddress_TCP,
				Address:  bindAddress,
				PortSpecifier: &envoy_config_core_v3.SocketAddress_PortValue{
					PortValue: port,
				},
				// As of Envoy 1.22: https://www.envoyproxy.io/docs/envoy/latest/version_history/v1.22/v1.22.0.html
				// the Ipv4Compat flag can only be set on Ipv6 address and Ipv4-mapped Ipv6 address.
				// Check if this is a non-padded pure ipv4 address and unset the compat flag if so.
				Ipv4Compat: !isIpv4Address,
			},
		},
	}
}

func tlsInspectorFilter() *envoy_config_listener_v3.ListenerFilter {
	configEnvoy := &envoy_tls_inspector.TlsInspector{}
	msg, _ := utils.MessageToAny(configEnvoy)
	return &envoy_config_listener_v3.ListenerFilter{
		Name: wellknown.TlsInspector,
		ConfigType: &envoy_config_listener_v3.ListenerFilter_TypedConfig{
			TypedConfig: msg,
		},
	}
}

func (h *filterChainTranslator) initFilterChain(ctx context.Context, fcc ir.FilterChainCommon, reporter reports.ListenerReporter) *envoy_config_listener_v3.FilterChain {
	info := &FilterChainInfo{
		Match: fcc.Matcher,
		TLS:   fcc.TLS,
	}

	fc := &envoy_config_listener_v3.FilterChain{
		Name:             fcc.FilterChainName,
		FilterChainMatch: info.toMatch(),
		TransportSocket:  info.toTransportSocket(),
	}

	return fc
}

func (h *filterChainTranslator) computeHttpFilters(ctx context.Context, l ir.HttpFilterChainIR, reporter reports.ListenerReporter) []*envoy_config_listener_v3.Filter {
	log := contextutils.LoggerFrom(ctx).Desugar()

	// 1. Generate all the network filters (including the HttpConnectionManager)
	networkFilters, err := h.computeNetworkFiltersForHttp(ctx, l, reporter)
	if err != nil {
		log.DPanic("error computing network filters", zap.Error(err))
		// TODO: report? return error?
		return nil
	}
	if len(networkFilters) == 0 {
		return nil
	}

	return networkFilters
}

func (n *filterChainTranslator) computeNetworkFiltersForHttp(ctx context.Context, l ir.HttpFilterChainIR, reporter reports.ListenerReporter) ([]*envoy_config_listener_v3.Filter, error) {
	hcm := hcmNetworkFilterTranslator{
		routeConfigName: n.routeConfigName,
		PluginPass:      n.PluginPass,
		reporter:        reporter,
		gateway:         n.gateway, // corresponds to Gateway API listener
	}
	networkFilters := sortNetworkFilters(n.computeCustomFilters(ctx, l.CustomNetworkFilters, reporter))
	networkFilter, err := hcm.computeNetworkFilters(ctx, l)
	if err != nil {
		return nil, err
	}
	networkFilters = append(networkFilters, networkFilter)
	return networkFilters, nil
}

// computeCustomFilters computes all custom filters, first from plugins, second
// from embedded filters on the FilterChain itself.
// For HTTP FilterChains these must be added before HCM.
func (n *filterChainTranslator) computeCustomFilters(
	ctx context.Context,
	customNetworkFilters []ir.CustomEnvoyFilter,
	reporter reports.ListenerReporter,
) []plugins.StagedNetworkFilter {
	var networkFilters []plugins.StagedNetworkFilter
	// Process the network filters.
	for _, plug := range n.PluginPass {
		stagedFilters, err := plug.NetworkFilters(ctx)
		if err != nil {
			reporter.SetCondition(reports.ListenerCondition{
				Type:    gwv1.ListenerConditionProgrammed,
				Reason:  gwv1.ListenerReasonInvalid,
				Status:  metav1.ConditionFalse,
				Message: "Error processing network plugin: " + err.Error(),
			})
			// TODO: return error?
		}

		for _, nf := range stagedFilters {
			if nf.Filter == nil {
				continue
			}
			networkFilters = append(networkFilters, nf)
		}
	}
	networkFilters = append(networkFilters, convertCustomNetworkFilters(customNetworkFilters)...)
	return networkFilters
}

func convertCustomNetworkFilters(customNetworkFilters []ir.CustomEnvoyFilter) []plugins.StagedNetworkFilter {
	var out []plugins.StagedNetworkFilter
	for _, customFilter := range customNetworkFilters {
		out = append(out, plugins.StagedNetworkFilter{
			Filter: &envoy_config_listener_v3.Filter{
				Name: customFilter.Name,
				ConfigType: &envoy_config_listener_v3.Filter_TypedConfig{
					TypedConfig: customFilter.Config,
				},
			},
			Stage: customFilter.FilterStage,
		})
	}
	return out
}

func sortNetworkFilters(filters plugins.StagedNetworkFilterList) []*envoy_config_listener_v3.Filter {
	sort.Sort(filters)
	var sortedFilters []*envoy_config_listener_v3.Filter
	for _, filter := range filters {
		sortedFilters = append(sortedFilters, filter.Filter)
	}
	return sortedFilters
}

type hcmNetworkFilterTranslator struct {
	routeConfigName string
	PluginPass      TranslationPassPlugins
	reporter        reports.ListenerReporter
	listener        ir.HttpFilterChainIR // policies attached to listener
	gateway         ir.GatewayIR         // policies attached to gateway
}

func (h *hcmNetworkFilterTranslator) computeNetworkFilters(ctx context.Context, l ir.HttpFilterChainIR) (*envoy_config_listener_v3.Filter, error) {
	ctx = contextutils.WithLogger(ctx, "compute_http_connection_manager")

	// 1. Initialize the HttpConnectionManager (HCM)
	httpConnectionManager := h.initializeHCM()

	// 2. Apply HttpFilters
	var err error
	httpConnectionManager.HttpFilters = h.computeHttpFilters(ctx, l)

	luaCode := `
package.path = "/usr/local/bin/?.lua;" .. package.path

-- 然后加载dkjson库（注意模块名与文件名一致）
local json = require("dkjson")

function envoy_on_request(request_handle)
    -- 获取 Authorization 头部
    local auth_header = request_handle:headers():get("authorization")
    if auth_header then
        -- 记录原始头部值
        local raw_value = auth_header

        -- 正则表达式匹配 Bearer Token（忽略大小写和空格）
        local pattern = "^%s*[Bb][Ee][Aa][Rr][Ee][Rr]%s+([A-Za-z0-9%-]+)%s*$"
        local apikey = auth_header:match(pattern)

        if apikey then
            request_handle:headers():replace("authorization", apikey)
            request_handle:logInfo("apikey: "..apikey)
            request_handle:streamInfo():dynamicMetadata():set("kubegien.org", "authorization", apikey)
        else
            -- 删除无效的 Authorization 头
            request_handle:headers():remove("authorization")
            request_handle:logWarn("invalid bearer format, value: "..raw_value)
        end
    end

    -- 获取请求体
    local body = request_handle:body()
    if not body or body:length() == 0 then
        request_handle:logInfo("not found body")
        return
    end

    -- 读取完整请求体内容,兼容性读取请求体
    local body_str
    if body.getString then          -- 新版本 Envoy 支持
        body_str = body:getString(0, body:length())
    else                            -- 旧版本 Envoy 兼容
        local body_bytes = body:getBytes(0, body:length())
        body_str = tostring(body_bytes)
    end

    -- 解析JSON
    local luaTable, pos, err = json.decode(body_str)
    if err then
        request_handle:logErr("json decode failed, body: "..body_str.." error: "..err)
        return
    end
    
    -- 提取model字段
    if luaTable and luaTable.model then
        local existing_header = request_handle:headers():get("model")
        -- 检查头是否存在再操作
        request_handle:logInfo("model: " .. luaTable.model)
        if existing_header  then
            request_handle:logInfo("replaced existing header")
            request_handle:headers():replace("model", luaTable.model)
        else
            request_handle:headers():add("model", luaTable.model)
        end
    else
        request_handle:logInfo("not found model")
    end

    -- 提取model字段
    if luaTable and luaTable.stream == true then
        -- 添加/更新stream_options
        luaTable.stream_options = luaTable.stream_options or {}
        luaTable.stream_options.include_usage = true

        -- 重新编码JSON并替换请求体
        local new_body = json.encode(luaTable)
        request_handle:body():setBytes(new_body)
        request_handle:logInfo("add stream_options")
    end
end

function envoy_on_response(response_handle)
    local usage_stats = { prompt_tokens=0, completion_tokens=0, total_tokens=0 }
    local body_str = ""
    local is_streaming = false

    -- 精准判断流式模式, 统一小写处理并清除空格干扰
    local content_type = (response_handle:headers():get("content-type") or ""):lower():gsub("%s*", "")

    if content_type:find("text/event%-stream") then
        is_streaming = true
        for chunk in response_handle:bodyChunks() do
            local chunk_str = chunk:getBytes(0, chunk:length()) or ""
            if chunk_str:match("\"usage\":") then
                body_str = chunk:getBytes(0, chunk:length())
            end
        end
    else
        local body = response_handle:body()
        if not body or body:length() == 0 then
            response_handle:logInfo("not found body")
            return
        end

        if body.getString then          -- 新版本 Envoy 支持
            body_str = body:getString(0, body:length())
        else                            -- 旧版本 Envoy 兼容
            local body_bytes = body:getBytes(0, body:length())
            body_str = tostring(body_bytes)
        end
    end

    if is_streaming then
        response_handle:logInfo("raw chunk: "..body_str)
        if not body_str:match("\"usage\":") then
            return
        end
        
        for chunk in body_str:gmatch("data:%s*([^\r\n]*)") do
            -- 增强型 SSE 数据块解析
            chunk = chunk:gsub("^%s*(.-)%s*$", "%1")

            if not chunk:match("\"usage\":") then
                goto continue
            end

            if chunk == "" or chunk == "[DONE]" then
                goto continue
            end

            local luaTable, pos, err = json.decode(chunk)
            if err then
                response_handle:logErr("is_streaming json decode failed, body: "..chunk.." error: "..err)
                goto continue
            end

            if luaTable and luaTable.usage and type(luaTable.usage) == "table" then
                -- 合并usage字段（使用最后一次有效值）
                usage_stats.prompt_tokens = luaTable.usage.prompt_tokens or usage_stats.prompt_tokens
                usage_stats.completion_tokens = luaTable.usage.completion_tokens or usage_stats.completion_tokens
                usage_stats.total_tokens = luaTable.usage.total_tokens or usage_stats.total_tokens
            end
            
            ::continue::
        end
    else
        -- 解析JSON
        local luaTable, pos, err = json.decode(body_str)
        if err then
            response_handle:logErr("response json decode failed, body: "..body_str.." error: "..err)
            return
        end

        if luaTable and luaTable.usage and type(luaTable.usage) == "table" then
            usage_stats.prompt_tokens = luaTable.usage.prompt_tokens
            usage_stats.completion_tokens = luaTable.usage.completion_tokens
            usage_stats.total_tokens = luaTable.usage.total_tokens
        end
    end

    -- 设置响应
    local metadata_fields = { "prompt_tokens", "completion_tokens", "total_tokens" }

    local dynamic_metadata = response_handle:streamInfo():dynamicMetadata()
    for _, field in ipairs(metadata_fields) do
        local value = tostring(usage_stats[field])
        dynamic_metadata:set("kubegien.org", field, value)
        response_handle:logDebug("Set dynamic metadata ["..field.."]: "..value)
    end
end
`
	luaConfig := &luav3.Lua{
		DefaultSourceCode: &v3.DataSource{
			Specifier: &v3.DataSource_InlineString{
				InlineString: luaCode,
			},
		},
	}

	// 转换为Any类型
	luaAny, _ := anypb.New(luaConfig)
	luaAny.TypeUrl = "type.googleapis.com/envoy.extensions.filters.http.lua.v3.Lua"

	httpConnectionManager.HttpFilters = append([]*envoyhttp.HttpFilter{
		{
			Name: "http-lua",
			ConfigType: &envoyhttp.HttpFilter_TypedConfig{
				TypedConfig: luaAny,
			},
		},
	}, httpConnectionManager.HttpFilters...)
	pass := h.PluginPass

	// 3. Allow any HCM plugins to make their changes, with respect to any changes the core plugin made
	attachedPoliciesSlice := []ir.AttachedPolicies{
		h.gateway.AttachedHttpPolicies,
		l.AttachedPolicies,
	}
	for _, attachedPolicies := range attachedPoliciesSlice {
		for gk, pols := range attachedPolicies.Policies {
			pass := pass[gk]
			if pass == nil {
				// TODO: report user error - they attached a non http policy
				continue
			}
			for _, pol := range pols {
				pctx := &ir.HcmContext{
					Policy: pol.PolicyIr,
				}
				if err := pass.ApplyHCM(ctx, pctx, httpConnectionManager); err != nil {
					h.reporter.SetCondition(reports.ListenerCondition{
						Type:    gwv1.ListenerConditionProgrammed,
						Reason:  gwv1.ListenerReasonInvalid,
						Status:  metav1.ConditionFalse,
						Message: "Error processing HCM plugin: " + err.Error(),
					})
				}
			}
		}
	}
	// TODO: should we enable websockets by default?

	// 4. Generate the typedConfig for the HCM
	hcmFilter, err := NewFilterWithTypedConfig(wellknown.HTTPConnectionManager, httpConnectionManager)
	if err != nil {
		contextutils.LoggerFrom(ctx).DPanic("failed to convert proto message to struct")
		return nil, fmt.Errorf("failed to convert proto message to any: %w", err)
	}

	return hcmFilter, nil
}

func (h *hcmNetworkFilterTranslator) initializeHCM() *envoyhttp.HttpConnectionManager {
	statPrefix := h.listener.FilterChainName
	if statPrefix == "" {
		statPrefix = DefaultHttpStatPrefix
	}

	return &envoyhttp.HttpConnectionManager{
		CodecType:        envoyhttp.HttpConnectionManager_AUTO,
		StatPrefix:       statPrefix,
		NormalizePath:    wrapperspb.Bool(true),
		MergeSlashes:     true,
		UseRemoteAddress: wrapperspb.Bool(true),
		RouteSpecifier: &envoyhttp.HttpConnectionManager_Rds{
			Rds: &envoyhttp.Rds{
				ConfigSource: &envoy_config_core_v3.ConfigSource{
					ResourceApiVersion: envoy_config_core_v3.ApiVersion_V3,
					ConfigSourceSpecifier: &envoy_config_core_v3.ConfigSource_Ads{
						Ads: &envoy_config_core_v3.AggregatedConfigSource{},
					},
				},
				RouteConfigName: h.routeConfigName,
			},
		},
	}
}

func (h *hcmNetworkFilterTranslator) computeHttpFilters(ctx context.Context, l ir.HttpFilterChainIR) []*envoyhttp.HttpFilter {
	var httpFilters plugins.StagedHttpFilterList

	log := contextutils.LoggerFrom(ctx).Desugar()

	// run the HttpFilter Plugins
	for _, plug := range h.PluginPass {
		stagedFilters, err := plug.HttpFilters(ctx, l.FilterChainCommon)
		if err != nil {
			// what to do with errors here? ignore the listener??
			h.reporter.SetCondition(reports.ListenerCondition{
				Type:    gwv1.ListenerConditionProgrammed,
				Reason:  gwv1.ListenerReasonInvalid,
				Status:  metav1.ConditionFalse,
				Message: "Error processing http plugin: " + err.Error(),
			})
		}

		for _, httpFilter := range stagedFilters {
			if httpFilter.Filter == nil {
				log.Warn("HttpFilters() returned nil", zap.String("name", plug.Name))
				continue
			}
			httpFilters = append(httpFilters, httpFilter)
		}
	}
	httpFilters = append(httpFilters, convertCustomHttpFilters(l.CustomHTTPFilters)...)

	// https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/http/http_filters#filter-ordering
	// HttpFilter ordering determines the order in which the HCM will execute the filter.

	// 1. Sort filters by stage
	// "Stage" is the type we use to specify when a filter should be run
	envoyHttpFilters := sortHttpFilters(httpFilters)

	// 2. Configure the router filter
	// As outlined by the Envoy docs, the last configured filter has to be a terminal filter.
	// We set the Router filter (https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/router_filter#config-http-filters-router)
	// as the terminal filter in kgateway.
	routerV3 := routerv3.Router{}

	//	// TODO it would be ideal of SuppressEnvoyHeaders and DynamicStats could be moved out of here set
	//	// in a separate router plugin
	//	if h.listener.GetOptions().GetRouter().GetSuppressEnvoyHeaders().GetValue() {
	//		routerV3.SuppressEnvoyHeaders = true
	//	}
	//
	//	routerV3.DynamicStats = h.listener.GetOptions().GetRouter().GetDynamicStats()

	newStagedFilter, err := plugins.NewStagedFilter(
		wellknown.Router,
		&routerV3,
		plugins.AfterStage(plugins.RouteStage),
	)
	if err != nil {
		h.reporter.SetCondition(reports.ListenerCondition{
			Type:    gwv1.ListenerConditionProgrammed,
			Reason:  gwv1.ListenerReasonInvalid,
			Status:  metav1.ConditionFalse,
			Message: "Error processing http plugins: " + err.Error(),
		})
		// TODO: return false?
	}

	envoyHttpFilters = append(envoyHttpFilters, newStagedFilter.Filter)

	return envoyHttpFilters
}

func convertCustomHttpFilters(customHttpFilters []ir.CustomEnvoyFilter) []plugins.StagedHttpFilter {
	var out []plugins.StagedHttpFilter
	for _, customFilter := range customHttpFilters {
		stagedFilter := plugins.StagedHttpFilter{
			Filter: &envoyhttp.HttpFilter{
				Name: customFilter.Name,
				ConfigType: &envoyhttp.HttpFilter_TypedConfig{
					TypedConfig: customFilter.Config,
				},
			},
			Stage: customFilter.FilterStage,
		}
		out = append(out, stagedFilter)
	}
	return out
}

func sortHttpFilters(filters plugins.StagedHttpFilterList) []*envoyhttp.HttpFilter {
	sort.Sort(filters)
	var sortedFilters []*envoyhttp.HttpFilter
	for _, filter := range filters {
		sortedFilters = append(sortedFilters, filter.Filter)
	}
	return sortedFilters
}

func (h *filterChainTranslator) computeTcpFilters(ctx context.Context, l ir.TcpIR, reporter reports.ListenerReporter) []*envoy_config_listener_v3.Filter {
	networkFilters := sortNetworkFilters(h.computeCustomFilters(ctx, l.CustomNetworkFilters, reporter))

	cfg := &envoytcp.TcpProxy{
		StatPrefix: l.FilterChainName,
	}
	if len(l.BackendRefs) == 1 {
		cfg.ClusterSpecifier = &envoytcp.TcpProxy_Cluster{
			Cluster: l.BackendRefs[0].ClusterName,
		}
	} else {
		var wc envoytcp.TcpProxy_WeightedCluster
		for _, route := range l.BackendRefs {
			w := route.Weight
			if w == 0 {
				w = 1
			}
			wc.Clusters = append(wc.GetClusters(), &envoytcp.TcpProxy_WeightedCluster_ClusterWeight{
				Name:   route.ClusterName,
				Weight: w,
			})
		}
		cfg.ClusterSpecifier = &envoytcp.TcpProxy_WeightedClusters{
			WeightedClusters: &wc,
		}
	}

	tcpFilter, _ := NewFilterWithTypedConfig(wellknown.TCPProxy, cfg)

	return append(networkFilters, tcpFilter)
}

func NewFilterWithTypedConfig(name string, config proto.Message) (*envoy_config_listener_v3.Filter, error) {
	s := &envoy_config_listener_v3.Filter{
		Name: name,
	}

	if config != nil {
		marshalledConf, err := utils.MessageToAny(config)
		if err != nil {
			// this should NEVER HAPPEN!
			return &envoy_config_listener_v3.Filter{}, err
		}

		s.ConfigType = &envoy_config_listener_v3.Filter_TypedConfig{
			TypedConfig: marshalledConf,
		}
	}

	return s, nil
}

type SslConfig struct {
	Bundle     TlsBundle
	SniDomains []string
}
type TlsBundle struct {
	CA         []byte
	PrivateKey []byte
	CertChain  []byte
}

type FilterChainInfo struct {
	Match ir.FilterChainMatch
	TLS   *ir.TlsBundle
}

func (info *FilterChainInfo) toMatch() *envoy_config_listener_v3.FilterChainMatch {
	if info == nil {
		return nil
	}

	// if all fields are empty, return nil
	if len(info.Match.SniDomains) == 0 && info.Match.DestinationPort == nil && len(info.Match.PrefixRanges) == 0 {
		return nil
	}

	return &envoy_config_listener_v3.FilterChainMatch{
		ServerNames:     info.Match.SniDomains,
		DestinationPort: info.Match.DestinationPort,
		PrefixRanges:    info.Match.PrefixRanges,
	}
}

func (info *FilterChainInfo) toTransportSocket() *envoy_config_core_v3.TransportSocket {
	if info == nil {
		return nil
	}
	ssl := info.TLS
	if ssl == nil {
		return nil
	}

	common := &envoyauth.CommonTlsContext{
		// default params
		TlsParams:     &envoyauth.TlsParameters{},
		AlpnProtocols: ssl.AlpnProtocols,
	}

	common.TlsCertificates = []*envoyauth.TlsCertificate{
		{
			CertificateChain: bytesDataSource(ssl.CertChain),
			PrivateKey:       bytesDataSource(ssl.PrivateKey),
		},
	}

	//	var requireClientCert *wrappers.BoolValue
	//	if common.GetValidationContextType() != nil {
	//		requireClientCert = &wrappers.BoolValue{Value: !dc.GetOneWayTls().GetValue()}
	//	}

	// default alpn for downstreams.
	//	if len(common.GetAlpnProtocols()) == 0 {
	//		common.AlpnProtocols = []string{"h2", "http/1.1"}
	//	} else if len(common.GetAlpnProtocols()) == 1 && common.GetAlpnProtocols()[0] == AllowEmpty { // allow override for advanced usage to set to a dangerous setting
	//		common.AlpnProtocols = []string{}
	//	}

	out := &envoyauth.DownstreamTlsContext{
		CommonTlsContext: common,
	}
	typedConfig, _ := utils.MessageToAny(out)

	return &envoy_config_core_v3.TransportSocket{
		Name:       wellknown.TransportSocketTls,
		ConfigType: &envoy_config_core_v3.TransportSocket_TypedConfig{TypedConfig: typedConfig},
	}
}

func bytesDataSource(s []byte) *envoy_config_core_v3.DataSource {
	return &envoy_config_core_v3.DataSource{
		Specifier: &envoy_config_core_v3.DataSource_InlineBytes{
			InlineBytes: s,
		},
	}
}
