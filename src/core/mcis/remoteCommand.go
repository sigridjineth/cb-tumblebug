/*
Copyright 2019 The Cloud-Barista Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package mcis is to manage multi-cloud infra service
package mcis

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/cloud-barista/cb-tumblebug/src/core/common"
	"github.com/cloud-barista/cb-tumblebug/src/core/mcir"
	validator "github.com/go-playground/validator/v10"
	"golang.org/x/crypto/ssh"
)

// sshDefaultUserName is array for temporal constants
var sshDefaultUserName = []string{"cb-user", "ubuntu", "root", "ec2-user"}

// McisCmdReq is struct for remote command
type McisCmdReq struct {
	UserName string   `json:"userName" example:"cb-user" default:""`
	Command  []string `json:"command" validate:"required" example:"client_ip=$(echo $SSH_CLIENT | awk '{print $1}'); echo SSH client IP is: $client_ip"`
}

// TbMcisCmdReqStructLevelValidation is func to validate fields in McisCmdReq
func TbMcisCmdReqStructLevelValidation(sl validator.StructLevel) {

	// u := sl.Current().Interface().(McisCmdReq)

	// err := common.CheckString(u.Command)
	// if err != nil {
	// 	// ReportError(field interface{}, fieldName, structFieldName, tag, param string)
	// 	sl.ReportError(u.Command, "command", "Command", err.Error(), "")
	// }
}

// SshCmdResult is struct for SshCmd Result
type SshCmdResult struct { // Tumblebug
	McisId  string         `json:"mcisId"`
	VmId    string         `json:"vmId"`
	VmIp    string         `json:"vmIp"`
	Command map[int]string `json:"command"`
	Stdout  map[int]string `json:"stdout"`
	Stderr  map[int]string `json:"stderr"`
	Err     error          `json:"err"`
}

// RemoteCommandToMcis is func to command to all VMs in MCIS by SSH
func RemoteCommandToMcis(nsId string, mcisId string, subGroupId string, vmId string, req *McisCmdReq) ([]SshCmdResult, error) {

	err := common.CheckString(nsId)
	if err != nil {
		common.CBLog.Error(err)
		return nil, err
	}

	err = common.CheckString(mcisId)
	if err != nil {
		common.CBLog.Error(err)
		return nil, err
	}

	// returns InvalidValidationError for bad validation input, nil or ValidationErrors ( []FieldError )
	err = validate.Struct(req)
	if err != nil {

		// this check is only needed when your code could produce
		// an invalid value for validation such as interface with nil
		// value most including myself do not usually have code like this.
		if _, ok := err.(*validator.InvalidValidationError); ok {
			fmt.Println(err)
			temp := []SshCmdResult{}
			return temp, err
		}

		// for _, err := range err.(validator.ValidationErrors) {

		// 	fmt.Println(err.Namespace()) // can differ when a custom TagNameFunc is registered or
		// 	fmt.Println(err.Field())     // by passing alt name to ReportError like below
		// 	fmt.Println(err.StructNamespace())
		// 	fmt.Println(err.StructField())
		// 	fmt.Println(err.Tag())
		// 	fmt.Println(err.ActualTag())
		// 	fmt.Println(err.Kind())
		// 	fmt.Println(err.Type())
		// 	fmt.Println(err.Value())
		// 	fmt.Println(err.Param())
		// 	fmt.Println()
		// }

		temp := []SshCmdResult{}
		return temp, err
	}

	check, _ := CheckMcis(nsId, mcisId)

	if !check {
		temp := []SshCmdResult{}
		err := fmt.Errorf("The mcis " + mcisId + " does not exist.")
		return temp, err
	}

	/*
		type contentSub struct {
			McisId string `json:"mcisId"`
			VmId   string `json:"vmId"`
			VmIp   string `json:"vmIp"`
			Result  string `json:"result"`
		}
		var content struct {
			ResultArray []contentSub `json:"resultArray"`
		}
	*/
	//content := RestPostCmdMcisResponseWrapper{}

	vmList, err := ListVmId(nsId, mcisId)
	if err != nil {
		common.CBLog.Error(err)
		return nil, err
	}
	if subGroupId != "" {
		vmListInGroup, err := ListVmBySubGroup(nsId, mcisId, subGroupId)
		if err != nil {
			common.CBLog.Error(err)
			return nil, err
		}
		if vmListInGroup == nil {
			err := fmt.Errorf("No VM in " + subGroupId)
			return nil, err
		}
		vmList = vmListInGroup
	}

	if vmId != "" {
		vmList = []string{vmId}
	}

	//goroutine sync wg
	var wg sync.WaitGroup

	var resultArray []SshCmdResult

	for _, vmId := range vmList {
		wg.Add(1)
		go RunRemoteCommandAsync(&wg, nsId, mcisId, vmId, req.UserName, req.Command, &resultArray)
	}
	wg.Wait() //goroutine sync wg

	return resultArray, nil
}

// RunRemoteCommand is func to execute a SSH command to a VM (sync call)
func RunRemoteCommand(nsId string, mcisId string, vmId string, givenUserName string, cmds []string) (map[int]string, map[int]string, error) {

	// use privagte IP of the target VM
	_, targetVmIP, targetSshPort := GetVmIp(nsId, mcisId, vmId)
	targetUserName, targetPrivateKey, err := VerifySshUserName(nsId, mcisId, vmId, targetVmIP, targetSshPort, givenUserName)

	// Set Bastion SSH config (bastionEndpoint, userName, Private Key)
	bastionNodes, err := GetBastionNodes(nsId, mcisId, vmId)
	if err != nil {
		common.CBLog.Error(err)
		return map[int]string{}, map[int]string{}, err
	}
	bastionNode := bastionNodes.VmId[0]
	// use public IP of the bastion VM
	bastionIp, _, bastionSshPort := GetVmIp(nsId, mcisId, bastionNode)
	bastionUserName, bastionSshKey, err := VerifySshUserName(nsId, mcisId, bastionNode, bastionIp, bastionSshPort, givenUserName)
	bastionEndpoint := fmt.Sprintf("%s:%s", bastionIp, bastionSshPort)

	bastionSshInfo := sshInfo{
		EndPoint:   bastionEndpoint,
		UserName:   bastionUserName,
		PrivateKey: []byte(bastionSshKey),
	}

	fmt.Println("[SSH] " + mcisId + "." + vmId + "(" + targetVmIP + ")" + " with userName: " + targetUserName)
	for i, v := range cmds {
		fmt.Println("[SSH] cmd[" + fmt.Sprint(i) + "]: " + v)
	}

	// Set VM SSH config (targetEndpoint, userName, Private Key)
	targetEndpoint := fmt.Sprintf("%s:%s", targetVmIP, targetSshPort)
	targetSshInfo := sshInfo{
		EndPoint:   targetEndpoint,
		UserName:   targetUserName,
		PrivateKey: []byte(targetPrivateKey),
	}

	// Execute SSH
	stdoutResults, stderrResults, err := runSSH(bastionSshInfo, targetSshInfo, cmds)
	if err != nil {
		fmt.Printf("Error executing commands: %s\n", err)
		return stdoutResults, stderrResults, err
	}
	return stdoutResults, stderrResults, nil

}

// RunRemoteCommandAsync is func to execute a SSH command to a VM (async call)
func RunRemoteCommandAsync(wg *sync.WaitGroup, nsId string, mcisId string, vmId string, givenUserName string, cmd []string, returnResult *[]SshCmdResult) {

	defer wg.Done() //goroutine sync done

	vmIP, _, _ := GetVmIp(nsId, mcisId, vmId)
	// RunRemoteCommand
	stdoutResults, stderrResults, err := RunRemoteCommand(nsId, mcisId, vmId, givenUserName, cmd)

	sshResultTmp := SshCmdResult{}
	sshResultTmp.McisId = mcisId
	sshResultTmp.VmId = vmId
	sshResultTmp.VmIp = vmIP
	sshResultTmp.Command = make(map[int]string)
	for i, c := range cmd {
		sshResultTmp.Command[i] = c
	}

	if err != nil {
		sshResultTmp.Stdout = stdoutResults
		sshResultTmp.Stderr = stderrResults
		sshResultTmp.Err = err
		*returnResult = append(*returnResult, sshResultTmp)
	} else {
		fmt.Println("[Begin] SSH Output")
		fmt.Println(stdoutResults)
		fmt.Println("[End] SSH Output")

		sshResultTmp.Stdout = stdoutResults
		sshResultTmp.Stderr = stderrResults
		sshResultTmp.Err = nil
		*returnResult = append(*returnResult, sshResultTmp)
	}
}

// VerifySshUserName is func to verify SSH username
func VerifySshUserName(nsId string, mcisId string, vmId string, vmIp string, sshPort string, givenUserName string) (string, string, error) {

	// Disable the verification of SSH username (until bastion host is supported)

	// // find vaild username
	// userName, verifiedUserName, privateKey := GetVmSshKey(nsId, mcisId, vmId)
	// userNames := []string{
	// 	sshDefaultUserName[0],
	// 	userName,
	// 	givenUserName,
	// 	sshDefaultUserName[1],
	// 	sshDefaultUserName[2],
	// 	sshDefaultUserName[3],
	// }

	// theUserName := ""
	// cmd := "sudo ls"

	// if verifiedUserName != "" {
	// 	/* Code for strict check in advance with real SSH (but slow down speed)
	// 	fmt.Printf("\n[Check SSH] (%s) with userName: %s\n", vmIp, verifiedUserName)
	// 	_, err := RunRemoteCommand(vmIp, sshPort, verifiedUserName, privateKey, cmd)
	// 	if err != nil {
	// 		return "", "", fmt.Errorf("Cannot do ssh, with %s, %s", verifiedUserName, err.Error())
	// 	}*/
	// 	theUserName = verifiedUserName
	// 	fmt.Printf("[%s] is a valid UserName\n", theUserName)
	// 	return theUserName, privateKey, nil
	// }

	// // If we have a varified username, Retrieve ssh username from the given list will not be executed
	// fmt.Println("[Retrieve ssh username from the given list]")
	// for _, v := range userNames {
	// 	if v != "" {
	// 		fmt.Printf("[Check SSH] (%s) with userName: %s\n", vmIp, v)
	// 		_, err := RunRemoteCommand(vmIp, sshPort, v, privateKey, cmd)
	// 		if err != nil {
	// 			fmt.Printf("Cannot do ssh, with %s, %s", verifiedUserName, err.Error())
	// 		} else {
	// 			theUserName = v
	// 			fmt.Printf("[%s] is a valid UserName\n", theUserName)
	// 			break
	// 		}
	// 		time.Sleep(3 * time.Second)
	// 	}
	// }

	userName, _, privateKey := GetVmSshKey(nsId, mcisId, vmId)

	theUserName := ""
	if givenUserName != "" {
		theUserName = givenUserName
	} else if userName != "" {
		theUserName = userName
	} else {
		theUserName = sshDefaultUserName[0] // default username: cb-user
	}

	if theUserName == "" {
		err := fmt.Errorf("Could not find a valid username")
		common.CBLog.Error(err)
		return "", "", err
	}

	// Disable the verification of SSH username (until bastion host is supported)

	// if theUserName != "" {
	// 	err := UpdateVmSshKey(nsId, mcisId, vmId, theUserName)
	// 	if err != nil {
	// 		common.CBLog.Error(err)
	// 		return "", "", err
	// 	}
	// } else {
	// 	return "", "", fmt.Errorf("Could not find a valid username")
	// }

	return theUserName, privateKey, nil
}

// CheckConnectivity func checks if given port is open and ready
func CheckConnectivity(host string, port string) error {

	// retry: 5 times, sleep: 5 seconds. timeout for each Dial: 20 seconds
	retrycheck := 5
	timeout := time.Second * time.Duration(20)
	for i := 0; i < retrycheck; i++ {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), timeout)
		conn.Close()

		fmt.Println("[Check SSH Port]", host, ":", port)

		if err != nil {
			fmt.Println("SSH Port is NOT accessible yet. retry after 5 seconds sleep ", err)
		} else {
			// port is opened. return nil for error.
			fmt.Println("SSH Port is accessible")
			return nil
		}
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("SSH Port is NOT not accessible (5 trials)")
}

// GetVmSshKey is func to get VM SShKey
func GetVmSshKey(nsId string, mcisId string, vmId string) (string, string, string) {

	var content struct {
		SshKeyId string `json:"sshKeyId"`
	}

	//fmt.Println("[GetVmSshKey]" + vmId)
	key := common.GenMcisKey(nsId, mcisId, vmId)

	keyValue, err := common.CBStore.Get(key)
	if err != nil {
		common.CBLog.Error(err)
		err = fmt.Errorf("In GetVmSshKey(); CBStore.Get() returned an error.")
		common.CBLog.Error(err)
		// return nil, err
	}

	json.Unmarshal([]byte(keyValue.Value), &content)

	//fmt.Printf("%+v\n", content.SshKeyId)

	sshKey := common.GenResourceKey(nsId, common.StrSSHKey, content.SshKeyId)
	keyValue, _ = common.CBStore.Get(sshKey)
	var keyContent struct {
		Username         string `json:"username"`
		VerifiedUsername string `json:"verifiedUsername"`
		PrivateKey       string `json:"privateKey"`
	}
	json.Unmarshal([]byte(keyValue.Value), &keyContent)

	return keyContent.Username, keyContent.VerifiedUsername, keyContent.PrivateKey
}

// UpdateVmSshKey is func to update VM SShKey
func UpdateVmSshKey(nsId string, mcisId string, vmId string, verifiedUserName string) error {

	var content struct {
		SshKeyId string `json:"sshKeyId"`
	}
	fmt.Println("[GetVmSshKey]" + vmId)
	key := common.GenMcisKey(nsId, mcisId, vmId)
	keyValue, err := common.CBStore.Get(key)
	if err != nil {
		common.CBLog.Error(err)
		err = fmt.Errorf("In UpdateVmSshKey(); CBStore.Get() returned an error.")
		common.CBLog.Error(err)
		// return nil, err
	}

	json.Unmarshal([]byte(keyValue.Value), &content)

	sshKey := common.GenResourceKey(nsId, common.StrSSHKey, content.SshKeyId)
	keyValue, _ = common.CBStore.Get(sshKey)

	tmpSshKeyInfo := mcir.TbSshKeyInfo{}
	json.Unmarshal([]byte(keyValue.Value), &tmpSshKeyInfo)

	tmpSshKeyInfo.VerifiedUsername = verifiedUserName

	val, _ := json.Marshal(tmpSshKeyInfo)
	err = common.CBStore.Put(keyValue.Key, string(val))
	if err != nil {
		common.CBLog.Error(err)
		return err
	}
	return nil
}

// Internal functions for SSH
func init() {
	//cblog = config.Cblogger
}

type sshInfo struct {
	UserName   string // ex) root
	PrivateKey []byte // ex) -----BEGIN RSA PRIVATE KEY-----
	EndPoint   string // ex) node12:22
}

// runSSH func execute a command by SSH
func runSSH(bastionInfo sshInfo, targetInfo sshInfo, cmds []string) (map[int]string, map[int]string, error) {

	stdoutMap := make(map[int]string)
	stderrMap := make(map[int]string)

	// Parse the private key for the bastion host
	bastionSigner, err := ssh.ParsePrivateKey(bastionInfo.PrivateKey)
	if err != nil {
		return stdoutMap, stderrMap, err
	}

	// Create an SSH client configuration for the bastion host
	bastionConfig := &ssh.ClientConfig{
		User: bastionInfo.UserName,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(bastionSigner),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	// Parse the private key for the target host
	targetSigner, err := ssh.ParsePrivateKey(targetInfo.PrivateKey)
	if err != nil {
		return stdoutMap, stderrMap, err
	}

	// Create an SSH client configuration for the target host
	targetConfig := &ssh.ClientConfig{
		User: targetInfo.UserName,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(targetSigner),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	// Setup the bastion host connection
	bastionClient, err := ssh.Dial("tcp", bastionInfo.EndPoint, bastionConfig)
	if err != nil {
		return stdoutMap, stderrMap, err
	}
	defer bastionClient.Close()

	// Setup the actual SSH client through the bastion host
	conn, err := bastionClient.Dial("tcp", targetInfo.EndPoint)
	if err != nil {
		return stdoutMap, stderrMap, err
	}

	ncc, chans, reqs, err := ssh.NewClientConn(conn, targetInfo.EndPoint, targetConfig)
	if err != nil {
		return stdoutMap, stderrMap, err
	}
	client := ssh.NewClient(ncc, chans, reqs)
	defer client.Close()

	// Create a new SSH session
	session, err := client.NewSession()
	if err != nil {
		return stdoutMap, stderrMap, err
	}
	defer session.Close()

	// Run the commands
	for i, cmd := range cmds {
		// Create a new SSH session for each command
		session, err := client.NewSession()
		if err != nil {
			return stdoutMap, stderrMap, err
		}
		defer session.Close()

		// Capture the output
		var stdoutBuf, stderrBuf bytes.Buffer
		session.Stdout = &stdoutBuf
		session.Stderr = &stderrBuf

		// Run the command
		err = session.Run(cmd)
		if err != nil {
			stderrMap[i] = fmt.Sprintf("(%s)\nStderr: %s", err, stderrBuf.String())
			break // Stop if the command fails
		}

		stdoutMap[i] = stdoutBuf.String()
		stderrMap[i] = stderrBuf.String()
	}

	return stdoutMap, stderrMap, nil
}

// BastionInfo is struct for bastion info
type BastionInfo struct {
	VmId []string `json:"vmId"`
}

// SetBastionNodes func sets bastion nodes
func SetBastionNodes(nsId string, mcisId string, targetVmId string, bastionVmId string) (string, error) {
	vmObj, err := GetVmObject(nsId, mcisId, targetVmId)
	if err != nil {
		common.CBLog.Error(err)
		return "", err
	}

	res, err := mcir.GetResource(nsId, common.StrVNet, vmObj.VNetId)
	if err != nil {
		common.CBLog.Error(err)
		return "", err
	}

	tempVNetInfo, ok := res.(mcir.TbVNetInfo)
	if !ok {
		common.CBLog.Error(err)
		return "", err
	}

	// find subnet and append bastion node
	for i, subnetInfo := range tempVNetInfo.SubnetInfoList {
		if subnetInfo.Id == vmObj.SubnetId {
			for _, existingId := range subnetInfo.BastionNodeIds {
				if existingId == bastionVmId {
					return fmt.Sprintf("Bastion (ID: %s) already exists in subnet (ID: %s) in VNet (ID: %s).",
						bastionVmId, subnetInfo.Id, vmObj.VNetId), nil
				}
			}

			// Append bastionVmId only if it doesn't already exist.
			subnetInfo.BastionNodeIds = append(subnetInfo.BastionNodeIds, bastionVmId)
			tempVNetInfo.SubnetInfoList[i] = subnetInfo
			mcir.UpdateResourceObject(nsId, common.StrVNet, tempVNetInfo)

			return fmt.Sprintf("Successfully set the bastion (ID: %s) for subnet (ID: %s) in vNet (ID: %s) for VM (ID: %s) in MCIS (ID: %s).",
				bastionVmId, subnetInfo.Id, vmObj.VNetId, targetVmId, mcisId), nil
		}
	}
	return "", fmt.Errorf("failed to set bastion. Subnet (ID: %s) not found in VNet (ID: %s) for VM (ID: %s) in MCIS (ID: %s) under namespace (ID: %s)",
		vmObj.SubnetId, vmObj.VNetId, targetVmId, mcisId, nsId)
}

// GetBastionNodes func retrieves bastion nodes for a given VM
func GetBastionNodes(nsId string, mcisId string, targetVmId string) (BastionInfo, error) {
	returnValue := BastionInfo{}
	// Fetch VM object based on nsId, mcisId, and targetVmId
	vmObj, err := GetVmObject(nsId, mcisId, targetVmId)
	if err != nil {
		common.CBLog.Error(err)
		return returnValue, err
	}

	// Fetch VNet resource information
	res, err := mcir.GetResource(nsId, common.StrVNet, vmObj.VNetId)
	if err != nil {
		common.CBLog.Error(err)
		return returnValue, err
	}

	// Type assertion for VNet information
	tempVNetInfo, ok := res.(mcir.TbVNetInfo)
	if !ok {
		common.CBLog.Error(err)
		return returnValue, err
	}

	// Find the subnet corresponding to the VM and return the BastionNodeIds
	for _, subnetInfo := range tempVNetInfo.SubnetInfoList {
		if subnetInfo.Id == vmObj.SubnetId {
			if subnetInfo.BastionNodeIds == nil {
				return returnValue, fmt.Errorf("no assigned bastion in Subnet (ID: %s) of VNet (ID: %s) for VM (ID: %s)",
					vmObj.SubnetId, vmObj.VNetId, targetVmId)
			}
			returnValue.VmId = subnetInfo.BastionNodeIds
			return returnValue, nil
		}
	}

	return returnValue, fmt.Errorf("failed to get bastion in Subnet (ID: %s) of VNet (ID: %s) for VM (ID: %s)",
		vmObj.SubnetId, vmObj.VNetId, targetVmId)
}
