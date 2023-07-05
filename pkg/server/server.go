package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"
	dpapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
	//dpapi "k8s.io/kubernetes/pkg/kubelet/apis/deviceplugin/v1beta1"
)

const (
	Namespace                  = "tdx.intel.com"
	DeviceType                 = "tdx-guest"
	TdxDpSocket                = "/var/lib/kubelet/device-plugins/tdxdp.sock"
	KubeletSocket              = "/var/lib/kubelet/device-plugins/kubelet.sock"
	TDX_DEVICE_DEPRECATED      = "/dev/tdx-attest"
	TDX_DEVICE_1_0             = "/dev/tdx-guest"
	TDX_DEVICE_1_5             = "/dev/tdx_guest"
	MaxRestartCount            = 5
	SocketConnectTimeout       = 5
	DefaultPodCount       uint = 110
)

type TdxDpServer struct {
	srv            *grpc.Server
	devices        map[string]*dpapi.Device
	ctx            context.Context
	cancel         context.CancelFunc
	restartFlag    bool
	tdxGuestDevice string
}

func NewTdxDpServer() *TdxDpServer {
	ctx, cancel := context.WithCancel(context.Background())
	return &TdxDpServer{
		devices:     make(map[string]*dpapi.Device),
		srv:         grpc.NewServer(grpc.EmptyServerOption{}),
		ctx:         ctx,
		cancel:      cancel,
		restartFlag: false,
	}
}

func (tdxdpsrv *TdxDpServer) getTdxVersion() error {

	if _, err := os.Stat(TDX_DEVICE_DEPRECATED); err == nil {
		return errors.New("Deprecated TDX device found")
	}

	if _, err := os.Stat(TDX_DEVICE_1_0); err == nil {
		tdxdpsrv.tdxGuestDevice = TDX_DEVICE_1_0
		return nil
	}

	if _, err := os.Stat(TDX_DEVICE_1_5); err == nil {
		tdxdpsrv.tdxGuestDevice = TDX_DEVICE_1_5
		return nil
	}

	return errors.New("No TDX device found")
}

func (tdxdpsrv *TdxDpServer) scanDevice() error {

	err := tdxdpsrv.getTdxVersion()
	if err != nil {
		return err
	}

	for i := uint(0); i < DefaultPodCount; i++ {
		deviceID := fmt.Sprintf("%s-%d", "tdx-guest", i)
		tdxdpsrv.devices[deviceID] = &dpapi.Device{
			ID:     deviceID,
			Health: dpapi.Healthy,
		}
	}

	return nil
}

func (tdxdpsrv *TdxDpServer) Run() error {

	err := tdxdpsrv.scanDevice()
	if err != nil {
		log.Fatalf("scan device error: %v", err)
	}

	dpapi.RegisterDevicePluginServer(tdxdpsrv.srv, tdxdpsrv)

	err = syscall.Unlink(TdxDpSocket)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	listen, err := net.Listen("unix", TdxDpSocket)
	if err != nil {
		return err
	}

	go func() {
		failCount := 0
		for {
			err = tdxdpsrv.srv.Serve(listen)
			if err == nil {
				break
			}

			if failCount > MaxRestartCount {
				log.Fatalf("TDX plugin server crashed. Quitting...")
			}
			failCount++
		}
	}()

	connection, err := tdxdpsrv.connect(TdxDpSocket, time.Duration(SocketConnectTimeout)*time.Second)
	if err != nil {
		return err
	}

	connection.Close()

	return nil
}

func (s *TdxDpServer) connect(unixSocketPath string, timeout time.Duration) (*grpc.ClientConn, error) {

	connection, err := grpc.Dial(unixSocketPath, grpc.WithInsecure(), grpc.WithBlock(),
		grpc.WithTimeout(timeout),
		grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout("unix", addr, timeout)
		}),
	)
	if err != nil {
		return nil, err
	}

	return connection, nil
}

func (tdxdpsrv *TdxDpServer) RegisterToKubelet() error {

	conn, err := tdxdpsrv.connect(KubeletSocket, time.Duration(MaxRestartCount)*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := dpapi.NewRegistrationClient(conn)
	request := &dpapi.RegisterRequest{
		Version:      dpapi.Version,
		Endpoint:     path.Base(TdxDpSocket),
		ResourceName: Namespace + "/" + DeviceType,
	}

	_, err = client.Register(context.Background(), request)
	if err != nil {
		return err
	}

	return nil
}

func (tdxdpsrv *TdxDpServer) ListAndWatch(e *dpapi.Empty, lwSrv dpapi.DevicePlugin_ListAndWatchServer) error {
	tdxDevices := make([]*dpapi.Device, len(tdxdpsrv.devices))

	i := 0
	for _, tdxDevice := range tdxdpsrv.devices {
		tdxDevices[i] = tdxDevice
		i++
	}

	err := lwSrv.Send(&dpapi.ListAndWatchResponse{Devices: tdxDevices})
	if err != nil {
		log.Fatalf("ListAndWatch error: %v", err)
		return err
	}

	for {
		select {
		case <-tdxdpsrv.ctx.Done():
			log.Println("ListAndWatch exit")
			return nil
		}
	}
}

func (tdxdpsrv *TdxDpServer) GetDevicePluginOptions(ctx context.Context, e *dpapi.Empty) (*dpapi.DevicePluginOptions, error) {
	return &dpapi.DevicePluginOptions{PreStartRequired: true}, nil
}

func (tdxdpsrv *TdxDpServer) GetPreferredAllocation(ctx context.Context, r *dpapi.PreferredAllocationRequest) (*dpapi.PreferredAllocationResponse, error) {
	/*devices := make(map[string]dpapi.Device)

	  for _, device := range tdxdpsrv.devices {
	          devices[device.ID] = *device
	  }

	  return tdxdpsrv.getPreferredAllocFunc(r, devices) */
	return &dpapi.PreferredAllocationResponse{}, nil
}

func (tdxdpsrv *TdxDpServer) PreStartContainer(ctx context.Context, req *dpapi.PreStartContainerRequest) (*dpapi.PreStartContainerResponse, error) {
	return &dpapi.PreStartContainerResponse{}, nil
}

func (tdxdpsrv *TdxDpServer) Allocate(ctx context.Context, reqs *dpapi.AllocateRequest) (*dpapi.AllocateResponse, error) {
	response := &dpapi.AllocateResponse{}
	for _, req := range reqs.ContainerRequests {
		log.Println("received request: ", strings.Join(req.DevicesIDs, ","))
		resp := dpapi.ContainerAllocateResponse{
			Envs: map[string]string{
				"TDX_DEVICES": strings.Join(req.DevicesIDs, ","),
			},
		}
		response.ContainerResponses = append(response.ContainerResponses, &resp)
	}
	return response, nil
}
