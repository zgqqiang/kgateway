package irtranslator

import (
	"encoding/json"

	envoy_config_cluster_v3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	envoy_config_listener_v3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	envoy_config_route_v3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// json marshalling for TranslationResult; used in tests

func (tr *TranslationResult) MarshalJSON() ([]byte, error) {
	m := protojson.MarshalOptions{
		Indent: "  ",
	}

	// Create a map to hold the marshaled fields
	result := make(map[string]interface{})

	// Marshal each field using protojson
	if len(tr.Routes) > 0 {
		routes, err := marshalProtoMessages(tr.Routes, m)
		if err != nil {
			return nil, err
		}
		result["Routes"] = routes
	}

	if len(tr.Listeners) > 0 {
		listeners, err := marshalProtoMessages(tr.Listeners, m)
		if err != nil {
			return nil, err
		}
		result["Listeners"] = listeners
	}

	if len(tr.ExtraClusters) > 0 {
		clusters, err := marshalProtoMessages(tr.ExtraClusters, m)
		if err != nil {
			return nil, err
		}
		result["ExtraClusters"] = clusters
	}

	// Marshal the result map to JSON
	return json.Marshal(result)
}

func marshalProtoMessages[T proto.Message](messages []T, m protojson.MarshalOptions) ([]interface{}, error) {
	var result []interface{}
	for _, msg := range messages {
		data, err := m.Marshal(msg)
		if err != nil {
			return nil, err
		}
		var jsonObj interface{}
		if err := json.Unmarshal(data, &jsonObj); err != nil {
			return nil, err
		}
		result = append(result, jsonObj)
	}
	return result, nil
}

func (tr *TranslationResult) UnmarshalJSON(data []byte) error {
	m := protojson.UnmarshalOptions{}

	// Create a map to hold the unmarshaled fields
	result := make(map[string]json.RawMessage)

	// Unmarshal the JSON data into the map
	if err := json.Unmarshal(data, &result); err != nil {
		return err
	}

	// Unmarshal each field using protojson
	if routesData, ok := result["Routes"]; ok {
		var routes []json.RawMessage
		if err := json.Unmarshal(routesData, &routes); err != nil {
			return err
		}
		tr.Routes = make([]*envoy_config_route_v3.RouteConfiguration, len(routes))
		for i, routeData := range routes {
			route := &envoy_config_route_v3.RouteConfiguration{}
			if err := m.Unmarshal(routeData, route); err != nil {
				return err
			}
			tr.Routes[i] = route
		}
	}

	if listenersData, ok := result["Listeners"]; ok {
		var listeners []json.RawMessage
		if err := json.Unmarshal(listenersData, &listeners); err != nil {
			return err
		}
		tr.Listeners = make([]*envoy_config_listener_v3.Listener, len(listeners))
		for i, listenerData := range listeners {
			listener := &envoy_config_listener_v3.Listener{}
			if err := m.Unmarshal(listenerData, listener); err != nil {
				return err
			}
			tr.Listeners[i] = listener
		}
	}

	if clustersData, ok := result["ExtraClusters"]; ok {
		var clusters []json.RawMessage
		if err := json.Unmarshal(clustersData, &clusters); err != nil {
			return err
		}
		tr.ExtraClusters = make([]*envoy_config_cluster_v3.Cluster, len(clusters))
		for i, clusterData := range clusters {
			cluster := &envoy_config_cluster_v3.Cluster{}
			if err := m.Unmarshal(clusterData, cluster); err != nil {
				return err
			}
			tr.ExtraClusters[i] = cluster
		}
	}

	return nil
}
