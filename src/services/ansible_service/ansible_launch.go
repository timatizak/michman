package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"gitlab.at.ispras.ru/openstack_bigdata_tools/spark-openstack/src/database"
	protobuf "gitlab.at.ispras.ru/openstack_bigdata_tools/spark-openstack/src/protobuf"
	"gitlab.at.ispras.ru/openstack_bigdata_tools/spark-openstack/src/utils"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	jupyterPort = "8888"
)

type InterfaceMap map[string]interface{}

type ServiceExists struct {
	exists  bool
	service *protobuf.Service
}

func GetElasticConnectorJar() string {
	elasticHadoopUrl := "http://download.elastic.co/hadoop/elasticsearch-hadoop-5.5.0.zip"
	elasticHadoopFilename := filepath.Join("/tmp", filepath.Base(elasticHadoopUrl))
	elasticDir := filepath.Join("/tmp", "elasticsearch-hadoop/")
	archivePath := "elasticsearch-hadoop-5.5.0/dist/elasticsearch-hadoop-5.5.0.jar"
	elasticPath := filepath.Join(elasticDir, archivePath)
	if _, err := os.Stat(elasticPath); err != nil {
		if os.IsNotExist(err) {
			// file does not exist
			log.Print("Downloading ElasticSearch Hadoop integration")
			utils.DownloadFile(elasticHadoopUrl, elasticHadoopFilename)

			if _, err := utils.Unzip(elasticHadoopFilename, elasticDir); err != nil {
				log.Print(err)
			}
		}
	}
	return elasticPath
}

func GetCassandraConnectorJar(sparkVersion string) string {
	var sparkCassandraConnectorUrl string
	if strings.HasPrefix(sparkVersion, "1.6") {
		sparkCassandraConnectorUrl = "http://dl.bintray.com/spark-packages/maven/datastax/spark-cassandra-connector/1.6.8-s_2.10/spark-cassandra-connector-1.6.8-s_2.10.jar"
	} else {
		sparkCassandraConnectorUrl = "http://dl.bintray.com/spark-packages/maven/datastax/spark-cassandra-connector/2.0.3-s_2.11/spark-cassandra-connector-2.0.3-s_2.11.jar"
	}
	sparkCassandraConnectorFile := filepath.Join("/tmp", filepath.Base(sparkCassandraConnectorUrl))

	//checking if file exists
	if _, err := os.Stat(sparkCassandraConnectorFile); err != nil {
		if os.IsNotExist(err) {
			// file does not exist
			log.Print("Downloading Spark Cassandra Connector for Spark version ", sparkVersion)
			utils.DownloadFile(sparkCassandraConnectorFile, sparkCassandraConnectorUrl)
		}
	}

	return sparkCassandraConnectorFile
}

func addJar(path string) map[string]string {
	var absPath string
	if v, err := filepath.Abs(path); err != nil {
		log.Fatalln(err)
	} else {
		absPath = v
	}
	var newElem = map[string]string{
		"name": filepath.Base(path), "path": absPath,
	}
	return newElem
}

func setDeployService(stype string) string {
	return "deploy_" + stype
}

func setServiceVersion(stype string) string {
	return stype + "_version"
}

func convertParamValue(value string, vType string) interface{}{
	switch vType {
	case "int":
		if v, err := strconv.ParseInt(value, 10, 32); err != nil {
			log.Print(err)
			return nil
		} else {
			return v
		}
	case "float":
		if v, err := strconv.ParseFloat(value, 64); err != nil {
			log.Print(err)
			return nil
		} else {
			return v
		}
	case "bool":
		if v, err := strconv.ParseBool(value); err != nil {
			log.Print(err)
			return nil
		} else {
			return v
		}
	case "string":
		return value
	}
	return nil
}


func makeExtraVars(aL AnsibleLauncher, cluster *protobuf.Cluster, osCreds *utils.OsCredentials, osConfig *utils.Config, action string) (InterfaceMap, error) {
	sTypes, err := aL.couchbaseCommunicator.ListServicesTypes()
	if err != nil {
		return nil, err
	}
	//appending old services which does not exist in new cluster configuration
	var curServices = make(map[string]ServiceExists)

	for _, service := range cluster.Services {
			curServices[service.Type] = ServiceExists{
				exists:  true,
				service: service,
			}
	}

	var extraVars = make(InterfaceMap)

	for _, st := range sTypes {
		if curServices[st.Type].exists {
			//set deploy_stype to True
			curS := curServices[st.Type].service
			extraVars[setDeployService(curS.Type)] = true

			//set service version
			if curS.Version != "" {
				extraVars[setServiceVersion(curS.Type)] = curS.Version
			} else {
				extraVars[setServiceVersion(curS.Type)] = st.DefaultVersion
			}

			//set version config params
			var curSv *protobuf.ServiceVersion
			for _, sv := range st.Versions {
				if sv.Version == extraVars[setServiceVersion(curS.Type)] {
					curSv = sv
					break
				}
			}

			for _, sc := range curSv.Configs {
				//check if in request presents current config param
				if value, ok := curS.Config[sc.ParameterName]; ok {
					extraVars[sc.AnsibleVarName] = convertParamValue(value, sc.Type)
				} else if sc.Required {
					//set default value if param is obligated
					extraVars[sc.AnsibleVarName] = convertParamValue(sc.DefaultValue, sc.Type)
				}
			}

		} else {
			extraVars[setDeployService(st.Type)] = false
		}
	}

	//filling obligated params
	extraVars["sync"] = "async" //must be always async mode
	extraVars["ansible_user"] = "ubuntu"

	extraVars["create_cluster"] = false
	if action == actionCreate {
		extraVars["create_cluster"] = true
	}

	extraVars["n_slaves"] = cluster.NHosts
	extraVars["cluster_name"] = cluster.Name

	//TODO: change this mode
	if res, ok := extraVars[setDeployService("nfs")]; ok && res == true {
		extraVars["create_storage"] = true
	}

	if res, ok := extraVars[setDeployService("nextcloud")]; ok && res == true {
		extraVars["create_storage"] = true
	}

	//if extraVars.DeployNFS || extraVars.DeployNextcloud {
	//	extraVars.CreateStorage = true
	//}

	extraVars["mountnfs"] = false
	extraVars["master_flavor"] = osConfig.MasterFlavor
	extraVars["slaves_flavor"] = osConfig.SlavesFlavor
	extraVars["storage_flavor"] = osConfig.StorageFlavor
	extraVars["boot_from_volume"] = false

	extraVars["hadoop_user"] = "ubuntu"

	extraVars["os_image"] = osConfig.OsImage
	extraVars["skip_packages"] = false
	extraVars["os_project_name"] = osCreds.OsProjectName
	extraVars["nfs_shares"] = []string{}

	extraVars["floating_ip_pool"] = osConfig.FloatingIP
	extraVars["os_auth_url"] = osCreds.OsAuthUrl
	extraVars["use_oracle_java"] = false //must be always false
	extraVars["ansible_ssh_private_key_file"] = utils.SshKeyPath

	//action must be "launch" in method "/clusters" POST and /clusters/{clusterName} PUT
	//action must be "destroy" in method /clusters/{clusterName} DELETE
	if action == actionCreate || action == actionUpdate {
		extraVars["act"] = utils.AnsibleLaunch
	} else if action == actionDelete {
		extraVars["act"] = utils.AnsibleDestroy
	}

	extraVars["virtual_network"] = osConfig.VirtualNetwork
	extraVars["os_key_name"] = osConfig.Key

	extraVars["os_swift_user_name"] = osCreds.OsSwiftUserName
	extraVars["os_swift_password"] = osCreds.OsSwiftPassword

	//make extra jars
	//TODO: change this
	var extraJars []map[string]string
	extraVars["spark_extra_jars"] = []map[string]string{}
	if extraVars[setDeployService("cassandra")] == true {
		cassandraJar := GetCassandraConnectorJar(extraVars["spark_version"].(string))
		extraJars = append(extraJars, addJar(cassandraJar))
	}

	//TODO: change this
	if extraVars[setDeployService("elastic")] == true {
		elasticJar := GetElasticConnectorJar()
		extraJars = append(extraJars, addJar(elasticJar))
	}

	if extraJars != nil {
		extraVars["spark_extra_jars"] = extraJars
	}

	//extraVars["spark_extra_jars"] = []map[string]string{}

	extraVars["use_mirror"] = osConfig.UseMirror
	enable, err := strconv.ParseBool(osConfig.UseMirror)
	if err != nil {
		log.Fatalln("use_mirror is not boolean")
	}
	if enable && !validateIP(osConfig.MirrorAddress) {
		log.Fatalln("ERROR: bad mirror's IP address")
	}

	extraVars["mirror_address"] = osConfig.MirrorAddress

	return extraVars, nil
}

type AnsibleLauncher struct {
	couchbaseCommunicator database.Database
}

func validateIP(input string) bool {
	pattern := "^(([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])\\.){3}([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])$"
	regEx := regexp.MustCompile(pattern)
	fmt.Println(input)
	return regEx.FindString(input) != ""
}

func findIP(input string) string {
	numBlock := "(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])"
	regexPattern := numBlock + "\\." + numBlock + "\\." + numBlock + "\\." + numBlock

	regEx := regexp.MustCompile(regexPattern)
	return regEx.FindString(input)
}

func setOsVars(osCreds *utils.OsCredentials, version string) error {
	switch version {
	case utils.OsSteinVersion:
		err := os.Setenv(utils.OsAuthUrl, osCreds.OsAuthUrl)
		if err != nil {
			log.Fatalln(err)
		}

		err = os.Setenv(utils.OsProjectName, osCreds.OsProjectName)
		if err != nil {
			log.Fatalln(err)
		}
		err = os.Setenv(utils.OsUsername, osCreds.OsUserName)
		if err != nil {
			log.Fatalln(err)
		}
		err = os.Setenv(utils.OsPassword, osCreds.OsPassword)
		if err != nil {
			log.Fatalln(err)
		}
		err = os.Setenv(utils.OsRegionName, osCreds.OsRegionName)
		if err != nil {
			log.Fatalln(err)
		}

		err = os.Setenv(utils.OsIdentityApiVersion, osCreds.OsIdentityApiVersion)
		if err != nil {
			log.Fatalln(err)
		}

		err = os.Setenv(utils.OsImageApiVersion, osCreds.OsImageApiVersion)
		if err != nil {
			log.Fatalln(err)
		}

		err = os.Setenv(utils.OsNoCache, osCreds.OsNoCache)
		if err != nil {
			log.Fatalln(err)
		}

		err = os.Setenv(utils.OsProjectDomainName, osCreds.OsProjectDomainName)
		if err != nil {
			log.Fatalln(err)
		}

		err = os.Setenv(utils.OsUserDomainName, osCreds.OsUserDomainName)
		if err != nil {
			log.Fatalln(err)
		}

		err = os.Setenv(utils.OsAuthType, osCreds.OsAuthType)
		if err != nil {
			log.Fatalln(err)
		}

		err = os.Setenv(utils.OsCloudname, osCreds.OsCloudname)
		if err != nil {
			log.Fatalln(err)
		}

		err = os.Setenv(utils.OsNovaVersion, osCreds.OsNovaVersion)
		if err != nil {
			log.Fatalln(err)
		}
		err = os.Setenv(utils.OsComputeApiVersion, osCreds.OsComputeApiVersion)
		if err != nil {
			log.Fatalln(err)
		}

		err = os.Setenv(utils.OsNoProxy, osCreds.OsNoProxy)
		if err != nil {
			log.Fatalln(err)
		}

		err = os.Setenv(utils.OsVolumeApiVersion, osCreds.OsVolumeApiVersion)
		if err != nil {
			log.Fatalln(err)
		}

		err = os.Setenv(utils.OsPythonwarnings, osCreds.OsPythonwarnings)
		if err != nil {
			log.Fatalln(err)
		}
	case utils.OsLibertyVersion:
		err := os.Setenv(utils.OsAuthUrl, osCreds.OsAuthUrl)
		if err != nil {
			log.Fatalln(err)
		}

		err = os.Setenv(utils.OsProjectName, osCreds.OsProjectName)
		if err != nil {
			log.Fatalln(err)
		}
		err = os.Setenv(utils.OsUsername, osCreds.OsUserName)
		if err != nil {
			log.Fatalln(err)
		}
		err = os.Setenv(utils.OsPassword, osCreds.OsPassword)
		if err != nil {
			log.Fatalln(err)
		}
		err = os.Setenv(utils.OsRegionName, osCreds.OsRegionName)
		if err != nil {
			log.Fatalln(err)
		}

		err = os.Setenv(utils.OsTenantId, osCreds.OsTenantId)
		if err != nil {
			log.Fatalln(err)
		}
		err = os.Setenv(utils.OsTenantName, osCreds.OsTenantName)
		if err != nil {
			log.Fatalln(err)
		}

		if osCreds.OsSwiftUserName != "" {
			err = os.Setenv(utils.OsSwiftUsername, osCreds.OsSwiftUserName)
			if err != nil {
				log.Fatalln(err)
			}
		}

		if osCreds.OsSwiftPassword != "" {
			err = os.Setenv(utils.OsSwiftPassword, osCreds.OsSwiftPassword)
			if err != nil {
				log.Fatalln(err)
			}
		}
	default: //liberty as default version
		err := os.Setenv(utils.OsAuthUrl, osCreds.OsAuthUrl)
		if err != nil {
			log.Fatalln(err)
		}

		err = os.Setenv(utils.OsProjectName, osCreds.OsProjectName)
		if err != nil {
			log.Fatalln(err)
		}
		err = os.Setenv(utils.OsUsername, osCreds.OsUserName)
		if err != nil {
			log.Fatalln(err)
		}
		err = os.Setenv(utils.OsPassword, osCreds.OsPassword)
		if err != nil {
			log.Fatalln(err)
		}
		err = os.Setenv(utils.OsRegionName, osCreds.OsRegionName)
		if err != nil {
			log.Fatalln(err)
		}

		err = os.Setenv(utils.OsTenantId, osCreds.OsTenantId)
		if err != nil {
			log.Fatalln(err)
		}
		err = os.Setenv(utils.OsTenantName, osCreds.OsTenantName)
		if err != nil {
			log.Fatalln(err)
		}

		if osCreds.OsSwiftUserName != "" {
			err = os.Setenv(utils.OsSwiftUsername, osCreds.OsSwiftUserName)
			if err != nil {
				log.Fatalln(err)
			}
		}

		if osCreds.OsSwiftPassword != "" {
			err = os.Setenv(utils.OsSwiftPassword, osCreds.OsSwiftPassword)
			if err != nil {
				log.Fatalln(err)
			}
		}
	}

	return nil
}

func (aL AnsibleLauncher) Run(cluster *protobuf.Cluster, osCreds *utils.OsCredentials, dockRegCreds *utils.DockerCredentials, osConfig *utils.Config, action string) string {
	log.SetPrefix("ANSIBLE_LAUNCHER: ")

	// creating ansible-playbook commands according to cluster object

	//exporting ansible variables
	err := setOsVars(osCreds, osConfig.OsVersion)
	if err != nil {
		log.Fatalln(err)
	}

	//constructing ansible-playbook command
	newExtraVars, err := makeExtraVars(aL, cluster, osCreds, osConfig, action)
	if err != nil {
		log.Fatalln(err)
	}

	newAnsibleArgs, err := json.Marshal(newExtraVars)
	if err != nil {
		log.Fatalln(err)
	}
	log.Print("New extra vars:")
	log.Print(string(newAnsibleArgs))

	cmdName := utils.AnsiblePlaybookCmd
	//cmdArgs := []string{"-vvv", utils.AnsibleMainRole, "--extra-vars", string(ansibleArgs)}
	cmdArgs := []string{"-vvv", utils.AnsibleMainRole, "--extra-vars", string(newAnsibleArgs)}
	//saving cluster to database
	log.Print("Writing new cluster to db...")
	err = aL.couchbaseCommunicator.WriteCluster(cluster)
	if err != nil {
		log.Fatalln(err)
	}

	log.Print("Running ansible...")

	// create output log
	f, err := os.Create("logs/ansible_output.log")

	defer f.Close()
	ansibleCmd := exec.Command(cmdName, cmdArgs...)
	stdout, err := ansibleCmd.StdoutPipe()
	if err != nil {
		log.Fatalln(err)
	}
	stderr, err := ansibleCmd.StderrPipe()
	if err != nil {
		log.Fatalln(err)
	}

	stdoutScanner := bufio.NewScanner(stdout)
	stderrScanner := bufio.NewScanner(stderr)
	go func() {
		for stdoutScanner.Scan() {
			_, err := f.WriteString(stdoutScanner.Text() + "\n")
			if err != nil {
				log.Fatalln(err)
			}
		}
	}()
	go func() {
		for stderrScanner.Scan() {
			_, err = f.WriteString(stderrScanner.Text() + "\n")
			if err != nil {
				log.Fatalln(err)
			}
		}
	}()

	ansibleOk := true
	if err := ansibleCmd.Start(); err != nil {
		ansibleOk = false
		log.Print("Error: ", err)
	}

	if err := ansibleCmd.Wait(); err != nil {
		ansibleOk = false
		log.Print("Error: ", err)
	}

	//Get Master IP for Cluster create or update action and save it
	if ansibleOk && (action == actionCreate || action == actionUpdate) {

		var v = map[string]string{
			"cluster_name": cluster.Name,
		}

		ipExtraVars, err := json.Marshal(v)
		if err != nil {
			log.Fatalln(err)
		}

		cmdName := utils.AnsiblePlaybookCmd
		args := []string{"-v", utils.AnsibleMasterIpRole, "--extra-vars", string(ipExtraVars)}

		log.Print("Running ansible for getting master IP...")
		cmd := exec.Command(cmdName, args...)
		var outb bytes.Buffer
		cmd.Stdout = &outb

		if err := cmd.Start(); err != nil {
			log.Print("Error: ", err)
		}

		if err := cmd.Wait(); err != nil {
			ansibleOk = false
			log.Print("Error: ", err)
		}

		masterIp := findIP(outb.String())
		nfsIp := ""
		if newExtraVars["deploy_nfs"] == true {
			v = map[string]string{
				"cluster_name":  cluster.Name,
				"extended_role": "storage",
			}
			ipExtraVars, err = json.Marshal(v)
			if err != nil {
				log.Fatalln(err)
			}
			cmdName = utils.AnsiblePlaybookCmd
			args = []string{"-v", utils.AnsibleIpRole, "--extra-vars", string(ipExtraVars)}
			log.Print("Running ansible for getting NFS server IP...")
			cmd := exec.Command(cmdName, args...)
			var outb bytes.Buffer
			cmd.Stdout = &outb
			if err := cmd.Start(); err != nil {
				log.Print("Error: ", err)
			}

			if err := cmd.Wait(); err != nil {
				ansibleOk = false
				log.Print("Error: ", err)
			}
			nfsIp = findIP(outb.String())
		}
		//filling services URLs:
		if masterIp != "" {
			log.Print("Master IP is: ", masterIp)
			cluster.MasterIP = masterIp

			for i, service := range cluster.Services {
				if service.Type == utils.ServiceTypeJupyter {
					cluster.Services[i].URL = masterIp + ":" + jupyterPort
				}
			}

			log.Print("Saving master IP...")
			err = aL.couchbaseCommunicator.WriteCluster(cluster)
			if err != nil {
				log.Fatalln(err)
			}
		} else {
			log.Print("There is no IP in Ansible output")
		}
		if nfsIp != "" {
			log.Print("NFS server IP is: ", nfsIp)
			for i, service := range cluster.Services {
				if service.Type == utils.ServiceTypeNFS || service.Type == utils.ServiceTypeNextCloud {
					cluster.Services[i].ServiceURL = nfsIp
				}
			}
			log.Print("Saving NFS server IP...")
			err = aL.couchbaseCommunicator.WriteCluster(cluster)
			if err != nil {
				log.Fatalln(err)
			}
		}

	}

	if ansibleOk {
		log.Print("Launch: OK")
		return utils.AnsibleOk
	} else {
		log.Print("Ansible has failed, check logs for mor information.")
		return utils.AnsibleFail
	}
}
