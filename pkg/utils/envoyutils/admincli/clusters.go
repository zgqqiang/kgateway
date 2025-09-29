package admincli

import (
	adminv3 "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	anypb "google.golang.org/protobuf/types/known/anypb"
)

// GetStaticClustersByName returns a map of static clusters, indexed by their name
// If there are no static clusters present, an empty map is returned
// An error is returned if any conversion fails
func GetStaticClustersByName(configDump *adminv3.ConfigDump) (map[string]*clusterv3.Cluster, error) {
	clustersByName := make(map[string]*clusterv3.Cluster, 10)
	for _, c := range configDump.GetConfigs() {
		staticCluster, err := convertToStaticCluster(c)
		if err != nil {
			return nil, err
		}
		cluster, err := convertToCluster(staticCluster.GetCluster())
		if err != nil {
			return nil, err
		}
		clustersByName[cluster.GetName()] = cluster
	}

	return clustersByName, nil
}

func convertToStaticCluster(a *anypb.Any) (*adminv3.ClustersConfigDump_StaticCluster, error) {
	var staticCluster adminv3.ClustersConfigDump_StaticCluster
	err := a.UnmarshalTo(&staticCluster)
	if err != nil {
		// We do not expect this to ever happen
		return nil, err
	}
	return &staticCluster, nil
}

func convertToCluster(a *anypb.Any) (*clusterv3.Cluster, error) {
	var cluster clusterv3.Cluster
	err := a.UnmarshalTo(&cluster)
	if err != nil {
		// We do not expect this to ever happen
		return nil, err
	}
	return &cluster, nil
}
