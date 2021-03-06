package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"google.golang.org/grpc"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	auth "github.com/envoyproxy/go-control-plane/envoy/service/auth/v2"
	"github.com/golang/protobuf/jsonpb"
	"google.golang.org/genproto/googleapis/rpc/status"
)

// empty struct because this isn't a fancy example
type AuthorizationServer struct{}

// inject a header that can be used for future rate limiting
func (a *AuthorizationServer) Check(ctx context.Context, req *auth.CheckRequest) (*auth.CheckResponse, error) {

	httpRequest := req.Attributes.Request.Http
	socketAddress := req.Attributes.Source.Address.GetSocketAddress()
	fmt.Printf("Source IP:port %s:%d\n", socketAddress.GetAddress(), socketAddress.GetPortValue())

	process := findProcessSourcePort(socketAddress.GetPortValue())
	if process == nil {
		process = &Process{
			User:        "Unknown",
			Name:        "Unknown",
			Pid:         "Unknown",
			Exe:         "Unknown",
			State:       "Unknown",
			Ip:          "Unknown",
			Port:        0,
			ForeignIp:   "Unknown",
			ForeignPort: 0,
		}
	}
	fmt.Printf("Process name: %s \n", process.Name)
	fmt.Printf("Process Exe: %s \n", process.Exe)
	fmt.Printf("Process User: %s \n", process.User)
	fmt.Printf("Process State: %s \n", process.State)
	marshaler := jsonpb.Marshaler{}
	jsonString, _ := marshaler.MarshalToString(httpRequest)
	var out bytes.Buffer
	err := json.Indent(&out, []byte(jsonString), "", "  ")
	if err == nil {
		println(out.String())
		return &auth.CheckResponse{
			Status: &status.Status{
				Code: 0,
			},
			HttpResponse: &auth.CheckResponse_OkResponse{
				OkResponse: &auth.OkHttpResponse{
					Headers: []*core.HeaderValueOption{
						{
							Header: &core.HeaderValue{
								Key:   "x-workload-id",
								Value: process.Name,
							},
						},
						{
							Header: &core.HeaderValue{
								Key:   "x-workload-user",
								Value: process.User,
							},
						},
						{
							Header: &core.HeaderValue{
								Key:   "x-workload-local-hostname",
								Value: awsMeta.localHostname,
							},
						},
						{
							Header: &core.HeaderValue{
								Key:   "x-workload-instance-id",
								Value: awsMeta.instanceId,
							},
						},
						{
							Header: &core.HeaderValue{
								Key:   "x-workload-zone",
								Value: awsMeta.zone,
							},
						},
					},
				},
			},
		}, nil
	} else {
		println("Error encoding JSON: " + err.Error())
		return &auth.CheckResponse{
			HttpResponse: &auth.CheckResponse_DeniedResponse{},
		}, nil
	}
}

type AwsMeta struct {
	localHostname string
	instanceId    string
	zone          string
}

const metaDataUrl string = "http://169.254.169.254/latest/meta-data/"

var awsMeta = &AwsMeta{}

func getMetadata(metaUrl string) string {
	var httpClient = &http.Client{
		Timeout: time.Second * 1,
	}
	resp, err := httpClient.Get(metaUrl)
	if err != nil {
		log.Fatalf("failed to get Zone information: %s", err)
	}
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("failed to read response body: %s", err)
	}
	bodyString := string(bodyBytes)
	err = resp.Body.Close()
	if err != nil {
		log.Fatalf("failed to close http body: %s", err)
	}
	return bodyString
}

func getAwsMetadataWithin(aws *AwsMeta) {
	zone := "placement/availability-zone/"
	localHost := "local-hostname"
	instance := "instance-id"

	aws.zone = getMetadata(metaDataUrl + zone)
	aws.localHostname = getMetadata(metaDataUrl + localHost)
	aws.instanceId = getMetadata(metaDataUrl + instance)
}

func main() {

	// Specific to AWS
	getAwsMetadataWithin(awsMeta)
	// create a TCP listener on port 5010
	lis, err := net.Listen("tcp", ":5010")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	log.Printf("listening on %s", lis.Addr())

	grpcServer := grpc.NewServer()
	authServer := &AuthorizationServer{}
	auth.RegisterAuthorizationServer(grpcServer, authServer)

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
