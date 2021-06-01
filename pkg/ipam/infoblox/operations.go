package infoblox

import (
	"fmt"
	"net"

	"github.com/go-logr/logr"
	ibclient "github.com/infobloxopen/infoblox-go-client"
	"github.com/spf13/viper"
)

type Manager struct {
	Log             logr.Logger
	ibConnectorFunc func() (*ibclient.Connector, error)
}

func (m *Manager) defaultIBConnectorFunc() (*ibclient.Connector, error) {
	hostConfig := ibclient.HostConfig{
		Host:     viper.GetString("ipam.infoblox.host"),
		Version:  viper.GetString("ipam.infoblox.wapi_version"),
		Port:     viper.GetString("ipam.infoblox.port"),
		Username: viper.GetString("ipam.infoblox.username"),
		Password: viper.GetString("ipam.infoblox.password"),
	}

	transportConfig := ibclient.NewTransportConfig("false", 15, 10)
	requestBuilder := &ibclient.WapiRequestBuilder{}
	requestor := &ibclient.WapiHttpRequestor{}
	conn, err := ibclient.NewConnector(hostConfig, transportConfig, requestBuilder, requestor)
	if err != nil {
		m.Log.Error(err, "Could not establish a connection to Infoblox Client")
		return nil, err
	}
	return conn, nil
}

// Initializes a connection to Infoblox
func (m *Manager) getIBConnector() (*ibclient.Connector, error) {
	if m.ibConnectorFunc == nil {
		m.ibConnectorFunc = m.defaultIBConnectorFunc
	}
	return m.ibConnectorFunc()
}

// GetorAllocateIP retrieves a reserved IP address from the subnet, if no IP has been reserved, it reserves
// the next available IP address in the subnet.

func (m *Manager) GetOrAllocateIP(deviceFQDN, networkView string, subnet *net.IPNet) (net.IP, error) {
	log := m.Log.WithValues("subnet", subnet.String())
	conn, err := m.getIBConnector()
	if err != nil {
		log.Error(err, "Cannot initiate connection")
	}
	defer conn.Logout()
	objMgr := ibclient.NewObjectManager(conn, "myclient", "")
	objMgr.OmitCloudAttrs = true // Needs to be set for on-prem version of Infoblox

	hostRecord, err := objMgr.GetHostRecord(deviceFQDN)
	if err != nil {
		log.Error(err, "Could not get assigned IP address for cluster")
	}
	if hostRecord != nil {
		if addr := findIP(hostRecord.Ipv4Addrs, subnet); addr != nil {
			log.Info("IP Address already assigned to cluster")
			return addr, nil
		}
	}

	log.Info("No IP allocated to cluster, allocating IP")
	if hostRecord != nil {
		// if a host record exists already, add a new address to it
		ipv4Addr := ibclient.NewHostRecordIpv4Addr(ibclient.HostRecordIpv4Addr{Ipv4Addr: fmt.Sprintf("func:nextavailableip:%s,%s", subnet.String(), networkView)})
		hostRecord.Ipv4Addrs = append(hostRecord.Ipv4Addrs, *ipv4Addr)
		ref, err := conn.UpdateObject(hostRecord, hostRecord.Ref)
		if err != nil {
			log.Error(err, "Could not allocate IP for cluster")
			return nil, err
		}
		if hostRecord, err = objMgr.GetHostRecordByRef(ref); err != nil {
			log.Error(err, "Could not allocate IP for cluster")
			return nil, err
		}
		return findIP(hostRecord.Ipv4Addrs, subnet), nil
	}

	// if there is no host record, create a new one
	ea := make(ibclient.EA)
	hostRecord, err = objMgr.CreateHostRecord(true, deviceFQDN, networkView, "default."+networkView, subnet.String(), "", "", ea)
	if err != nil {
		log.Error(err, "Could not allocate IP for cluster")
		return nil, err
	}
	log.Info("IP address allocated successfully to cluster")
	return net.ParseIP(hostRecord.Ipv4Addr), err
}

func findIP(addrs []ibclient.HostRecordIpv4Addr, subnet *net.IPNet) net.IP {
	for _, addr := range addrs {
		a := net.ParseIP(addr.Ipv4Addr)
		if subnet.Contains(a) {
			return a
		}
	}
	return nil
}

// ReleaseIP releases a single IP address within a subnet that's assigned to a cluster.
func (m *Manager) ReleaseIP(deviceName, networkView string, subnet *net.IPNet) error {
	log := m.Log.WithValues("subnet", subnet.String())
	conn, err := m.getIBConnector()
	if err != nil {
		return err
	}
	defer conn.Logout()
	objMgr := ibclient.NewObjectManager(conn, "myclient", "")
	objMgr.OmitCloudAttrs = true // Needs to be set for on-prem version of Infoblox
	_, err = objMgr.ReleaseIP(networkView, subnet.String(), "", "")
	if err != nil {
		log.Error(err, "Could not release IP for cluster")
		return err
	}
	log.Info("IP address released for cluster")
	return err
}
