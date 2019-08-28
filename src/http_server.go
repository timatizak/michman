package main

import (
	"log"
	"net/http"
	"os"

	"github.com/julienschmidt/httprouter"
	grpc_client "gitlab.at.ispras.ru/openstack_bigdata_tools/spark-openstack/src/grpcclients"
	"gitlab.at.ispras.ru/openstack_bigdata_tools/spark-openstack/src/handlers"
)

const (
	addressAnsibleService = "localhost:5000"
	addressDBService      = "localhost:5001"
)

func main() {
	// creating grpc client for communicating with services
	grpcClientLogger := log.New(os.Stdout, "GRPC_CLIENT: ", log.Ldate|log.Ltime)
	gc := grpc_client.GrpcClient{}
	gc.SetLogger(grpcClientLogger)
	gc.SetConnection(addressAnsibleService, addressDBService)

	httpServerLogger := log.New(os.Stdout, "HTTP_SERVER: ", log.Ldate|log.Ltime)
	hS := handlers.HttpServer{Gc: gc, Logger: httpServerLogger}

	//http.HandleFunc("/clusters", hS.clustersHandler)
	//http.HandleFunc("/clusters/{clusterName}", hS.clustersByNameHandler)

	router := httprouter.New()
	router.GET("/clusters", hS.ClustersGetList)
	router.POST("/clusters", hS.ClusterCreate)
	router.GET("/clusters/:clusterName", hS.ClustersGet)
	router.PUT("/clusters/:clusterName", hS.ClustersUpdate)
	router.DELETE("/clusters/:clusterName", hS.ClustersDelete)

	httpServerLogger.Print("Server starts to work")
	httpServerLogger.Fatal(http.ListenAndServe(":8080", router))
}