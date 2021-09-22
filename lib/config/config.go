package config

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"msh/lib/logger"
)

var (
	// Config variables contain the configuration parameters for config file and runtime
	ConfigDefault configuration
	ConfigRuntime configuration

	// ServerIcon contains the minecraft server icon
	ServerIcon string

	// Listen and Target host/port used for proxy connection
	ListenHost string
	ListenPort string
	TargetHost string
	TargetPort string
)

// struct adapted to config.json
type configuration struct {
	Server struct {
		Folder   string `json:"Folder"`
		FileName string `json:"FileName"`
		Protocol string `json:"Protocol"`
		Version  string `json:"Version"`
	} `json:"Server"`
	Commands struct {
		StartServer         string `json:"StartServer"`
		StartServerParam    string `json:"StartServerParam"`
		StopServer          string `json:"StopServer"`
		StopServerAllowKill int    `json:"StopServerAllowKill"`
	} `json:"Commands"`
	Msh struct {
		Debug                         bool   `json:"Debug"`
		InfoHibernation               string `json:"InfoHibernation"`
		InfoStarting                  string `json:"InfoStarting"`
		NotifyUpdate                  bool   `json:"NotifyUpdate"`
		Port                          string `json:"Port"`
		TimeBeforeStoppingEmptyServer int64  `json:"TimeBeforeStoppingEmptyServer"`
	} `json:"Msh"`
}

// LoadConfig loads json data from config.json into config
func LoadConfig() error {
	logger.Logln("loading config file...")

	// read config.json
	configData, err := ioutil.ReadFile("config.json")
	if err != nil {
		return fmt.Errorf("loadConfig: %v", err)
	}

	// write read data into ConfigDefault
	err = json.Unmarshal(configData, &ConfigDefault)
	if err != nil {
		return fmt.Errorf("loadConfig: %v", err)
	}

	// generate runtime config
	ConfigRuntime = generateConfigRuntime()

	// --------------- ConfigRuntime --------------- //
	// from now on only ConfigRuntime should be used //

	err = checkConfigRuntime()
	if err != nil {
		return fmt.Errorf("loadConfig: %v", err)
	}

	// as soon as the Config variable is set, set debug level
	logger.Debug = ConfigRuntime.Msh.Debug

	// initialize ip and ports for connection
	ListenHost, ListenPort, TargetHost, TargetPort, err = getIpPorts()
	if err != nil {
		return fmt.Errorf("loadConfig: %v", err)
	}
	logger.Logln("msh proxy setup:\t", ListenHost+":"+ListenPort, "-->", TargetHost+":"+TargetPort)

	// set server icon
	ServerIcon, err = loadIcon(ConfigRuntime.Server.Folder)
	if err != nil {
		// it's enough to log it without returning
		// since the default icon is loaded by default
		logger.Logln("loadConfig:", err.Error())
	}

	return nil
}

// SaveConfigDefault saves ConfigDefault to the config file
func SaveConfigDefault() error {
	// write the struct config to json data
	configData, err := json.MarshalIndent(ConfigDefault, "", "  ")
	if err != nil {
		return fmt.Errorf("SaveConfig: could not marshal from config.json")
	}

	// write json data to config.json
	err = ioutil.WriteFile("config.json", configData, 0644)
	if err != nil {
		return fmt.Errorf("SaveConfig: could not write to config.json")
	}

	logger.Logln("SaveConfig: saved to config.json")

	return nil
}

// generateConfigRuntime parses start arguments into ConfigRuntime and replaces placeholders
func generateConfigRuntime() configuration {
	// initialize with ConfigDefault
	ConfigRuntime = ConfigDefault

	// specify arguments
	flag.StringVar(&ConfigRuntime.Server.FileName, "f", ConfigRuntime.Server.FileName, "Specify server file name.")
	flag.StringVar(&ConfigRuntime.Server.Folder, "F", ConfigRuntime.Server.Folder, "Specify server folder path.")

	flag.StringVar(&ConfigRuntime.Commands.StartServerParam, "P", ConfigRuntime.Commands.StartServerParam, "Specify start server parameters.")

	flag.StringVar(&ConfigRuntime.Msh.Port, "p", ConfigRuntime.Msh.Port, "Specify msh port.")
	flag.StringVar(&ConfigRuntime.Msh.InfoHibernation, "h", ConfigRuntime.Msh.InfoHibernation, "Specify hibernation info.")
	flag.StringVar(&ConfigRuntime.Msh.InfoStarting, "s", ConfigRuntime.Msh.InfoStarting, "Specify starting info.")
	flag.BoolVar(&ConfigRuntime.Msh.Debug, "d", ConfigRuntime.Msh.Debug, "Set debug to true.")

	// specify the usage when there is an error in the arguments
	flag.Usage = func() {
		fmt.Printf("Usage of msh:\n")
		flag.PrintDefaults()
	}

	// parse arguments
	flag.Parse()

	// replace placeholders in ConfigRuntime StartServer command
	ConfigRuntime.Commands.StartServer = strings.ReplaceAll(ConfigRuntime.Commands.StartServer, "<Server.FileName>", ConfigRuntime.Server.FileName)
	ConfigRuntime.Commands.StartServer = strings.ReplaceAll(ConfigRuntime.Commands.StartServer, "<Commands.StartServerParam>", ConfigRuntime.Commands.StartServerParam)

	return ConfigRuntime
}

// checkConfigRuntime checks different parameters in ConfigRuntime
func checkConfigRuntime() error {
	// check if serverFile/serverFolder exists
	// (if config.Basic.ServerFileName == "", then it will just check if the server folder exist)
	serverFileFolderPath := filepath.Join(ConfigRuntime.Server.Folder, ConfigRuntime.Server.FileName)
	_, err := os.Stat(serverFileFolderPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("checkConfig: specified server file/folder does not exist: %s", serverFileFolderPath)
	}

	// check if java is installed
	_, err = exec.LookPath("java")
	if err != nil {
		return fmt.Errorf("checkConfig: java not installed")
	}

	return nil
}

// getIpPorts reads server.properties server file and returns the correct ports
func getIpPorts() (string, string, string, string, error) {
	serverPropertiesFilePath := filepath.Join(ConfigRuntime.Server.Folder, "server.properties")
	data, err := ioutil.ReadFile(serverPropertiesFilePath)
	if err != nil {
		return "", "", "", "", fmt.Errorf("setIpPorts: %v", err)
	}

	dataStr := string(data)
	dataStr = strings.ReplaceAll(dataStr, "\r", "")
	TargetPort = strings.Split(strings.Split(dataStr, "server-port=")[1], "\n")[0]

	if TargetPort == ConfigRuntime.Msh.Port {
		return "", "", "", "", fmt.Errorf("setIpPorts: TargetPort and ListenPort appear to be the same, please change one of them")
	}

	// return ListenHost, TargetHost, TargetPort, nil
	return "0.0.0.0", ConfigRuntime.Msh.Port, "127.0.0.1", TargetPort, nil
}