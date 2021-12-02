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

// Package mcir is to manage multi-cloud infra resource
package mcir

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/cloud-barista/cb-spider/interface/api"
	"github.com/cloud-barista/cb-tumblebug/src/core/common"
	validator "github.com/go-playground/validator/v10"
	"github.com/go-resty/resty/v2"
)

// 2020-04-09 https://github.com/cloud-barista/cb-spider/blob/master/cloud-control-manager/cloud-driver/interfaces/resources/VPCHandler.go

type SpiderVPCReqInfoWrapper struct { // Spider
	ConnectionName string
	ReqInfo        SpiderVPCReqInfo
}

type SpiderVPCReqInfo struct { // Spider
	Name           string
	IPv4_CIDR      string
	SubnetInfoList []SpiderSubnetReqInfo
	//SubnetInfoList []SpiderSubnetInfo
}

type SpiderSubnetReqInfoWrapper struct { // Spider
	ConnectionName string
	ReqInfo        SpiderSubnetReqInfo
}

type SpiderSubnetReqInfo struct { // Spider
	Name         string `validate:"required"`
	IPv4_CIDR    string `validate:"required"`
	KeyValueList []common.KeyValue
}

type SpiderVPCInfo struct { // Spider
	IId            common.IID // {NameId, SystemId}
	IPv4_CIDR      string
	SubnetInfoList []SpiderSubnetInfo
	KeyValueList   []common.KeyValue
}

type SpiderSubnetInfo struct { // Spider
	IId          common.IID // {NameId, SystemId}
	IPv4_CIDR    string
	KeyValueList []common.KeyValue
}

type TbVNetReq struct { // Tumblebug
	Name           string        `json:"name" validate:"required"`
	ConnectionName string        `json:"connectionName" validate:"required"`
	CidrBlock      string        `json:"cidrBlock"`
	SubnetInfoList []TbSubnetReq `json:"subnetInfoList"`
	Description    string        `json:"description"`
}

func TbVNetReqStructLevelValidation(sl validator.StructLevel) {

	u := sl.Current().Interface().(TbVNetReq)

	err := common.CheckString(u.Name)
	if err != nil {
		// ReportError(field interface{}, fieldName, structFieldName, tag, param string)
		sl.ReportError(u.Name, "name", "Name", err.Error(), "")
	}
}

type TbVNetInfo struct { // Tumblebug
	Id                   string            `json:"id"`
	Name                 string            `json:"name"`
	ConnectionName       string            `json:"connectionName"`
	CidrBlock            string            `json:"cidrBlock"`
	SubnetInfoList       []TbSubnetInfo    `json:"subnetInfoList"`
	Description          string            `json:"description"`
	CspVNetId            string            `json:"cspVNetId"`
	CspVNetName          string            `json:"cspVNetName"`
	Status               string            `json:"status"`
	KeyValueList         []common.KeyValue `json:"keyValueList"`
	AssociatedObjectList []string          `json:"associatedObjectList"`
	IsAutoGenerated      bool              `json:"isAutoGenerated"`

	// SystemLabel is for describing the MCIR in a keyword (any string can be used) for special System purpose
	SystemLabel string `json:"systemLabel" example:"Managed by CB-Tumblebug" default:""`

	// Disabled for now
	//Region         string `json:"region"`
	//ResourceGroupName string `json:"resourceGroupName"`
}

type TbSubnetReq struct { // Tumblebug
	Name         string `validate:"required"`
	IPv4_CIDR    string `validate:"required"`
	KeyValueList []common.KeyValue
	Description  string
}

func TbSubnetReqStructLevelValidation(sl validator.StructLevel) {

	u := sl.Current().Interface().(TbSubnetReq)

	err := common.CheckString(u.Name)
	if err != nil {
		// ReportError(field interface{}, fieldName, structFieldName, tag, param string)
		sl.ReportError(u.Name, "name", "Name", err.Error(), "")
	}
}

type TbSubnetInfo struct { // Tumblebug
	Id           string
	Name         string `validate:"required"`
	IPv4_CIDR    string `validate:"required"`
	KeyValueList []common.KeyValue
	Description  string
}

// CreateVNet accepts vNet creation request, creates and returns an TB vNet object
func CreateVNet(nsId string, u *TbVNetReq, option string) (TbVNetInfo, error) {
	fmt.Println("=========================== CreateVNet")

	resourceType := common.StrVNet

	err := common.CheckString(nsId)
	if err != nil {
		temp := TbVNetInfo{}
		common.CBLog.Error(err)
		return temp, err
	}

	// returns InvalidValidationError for bad validation input, nil or ValidationErrors ( []FieldError )
	err = validate.Struct(u)
	if err != nil {

		// this check is only needed when your code could produce
		// an invalid value for validation such as interface with nil
		// value most including myself do not usually have code like this.
		if _, ok := err.(*validator.InvalidValidationError); ok {
			fmt.Println(err)
			temp := TbVNetInfo{}
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

		temp := TbVNetInfo{}
		return temp, err
	}

	check, err := CheckResource(nsId, resourceType, u.Name)

	if check {
		temp := TbVNetInfo{}
		err := fmt.Errorf("The vNet " + u.Name + " already exists.")
		return temp, err
	}

	if err != nil {
		temp := TbVNetInfo{}
		err := fmt.Errorf("Failed to check the existence of the vNet " + u.Name + ".")
		return temp, err
	}

	tempReq := SpiderVPCReqInfoWrapper{}
	tempReq.ConnectionName = u.ConnectionName
	tempReq.ReqInfo.Name = nsId + "-" + u.Name
	tempReq.ReqInfo.IPv4_CIDR = u.CidrBlock

	// tempReq.ReqInfo.SubnetInfoList = u.SubnetInfoList
	for _, v := range u.SubnetInfoList {
		jsonBody, err := json.Marshal(v)
		if err != nil {
			common.CBLog.Error(err)
		}

		spiderSubnetInfo := SpiderSubnetReqInfo{}
		err = json.Unmarshal(jsonBody, &spiderSubnetInfo)
		if err != nil {
			common.CBLog.Error(err)
		}

		tempReq.ReqInfo.SubnetInfoList = append(tempReq.ReqInfo.SubnetInfoList, spiderSubnetInfo)
	}

	var tempSpiderVPCInfo *SpiderVPCInfo

	if os.Getenv("SPIDER_CALL_METHOD") == "REST" {

		client := resty.New().SetCloseConnection(true)
		client.SetAllowGetMethodPayload(true)

		req := client.R().
			SetHeader("Content-Type", "application/json").
			SetBody(tempReq).
			SetResult(&SpiderVPCInfo{}) // or SetResult(AuthSuccess{}).
		//SetError(&AuthError{}).       // or SetError(AuthError{}).

		var resp *resty.Response
		var err error

		var url string
		if option == "register" {
			url = fmt.Sprintf("%s/vpc/%s", common.SpiderRestUrl, u.Name)
			resp, err = req.Get(url)
		} else {
			url = fmt.Sprintf("%s/vpc", common.SpiderRestUrl)
			resp, err = req.Post(url)
		}

		if err != nil {
			common.CBLog.Error(err)
			content := TbVNetInfo{}
			err := fmt.Errorf("an error occurred while requesting to CB-Spider")
			return content, err
		}

		fmt.Println("HTTP Status code: " + strconv.Itoa(resp.StatusCode()))
		switch {
		case resp.StatusCode() >= 400 || resp.StatusCode() < 200:
			err := fmt.Errorf(string(resp.Body()))
			common.CBLog.Error(err)
			content := TbVNetInfo{}
			return content, err
		}

		tempSpiderVPCInfo = resp.Result().(*SpiderVPCInfo)

	} else {

		// Set CCM API
		ccm := api.NewCloudResourceHandler()
		err := ccm.SetConfigPath(os.Getenv("CBTUMBLEBUG_ROOT") + "/conf/grpc_conf.yaml")
		if err != nil {
			common.CBLog.Error("ccm failed to set config : ", err)
			return TbVNetInfo{}, err
		}
		err = ccm.Open()
		if err != nil {
			common.CBLog.Error("ccm api open failed : ", err)
			return TbVNetInfo{}, err
		}
		defer ccm.Close()

		payload, _ := json.MarshalIndent(tempReq, "", "  ")
		fmt.Println("payload: " + string(payload)) // for debug

		// result, err := ccm.CreateVPC(string(payload))
		var result string

		if option == "register" {
			result, err = ccm.CreateVPC(string(payload))
		} else {
			result, err = ccm.GetVPC(string(payload))
		}

		if err != nil {
			common.CBLog.Error(err)
			return TbVNetInfo{}, err
		}

		tempSpiderVPCInfo = &SpiderVPCInfo{} // Spider
		err = json.Unmarshal([]byte(result), &tempSpiderVPCInfo)
		if err != nil {
			common.CBLog.Error(err)
			return TbVNetInfo{}, err
		}

	}

	content := TbVNetInfo{}
	//content.Id = common.GenUid()
	content.Id = u.Name
	content.Name = u.Name
	content.ConnectionName = u.ConnectionName
	content.CspVNetId = tempSpiderVPCInfo.IId.SystemId
	content.CspVNetName = tempSpiderVPCInfo.IId.NameId
	content.CidrBlock = tempSpiderVPCInfo.IPv4_CIDR
	content.Description = u.Description
	content.KeyValueList = tempSpiderVPCInfo.KeyValueList
	content.AssociatedObjectList = []string{}

	if option == "register" {
		content.SystemLabel = "Registered from CSP resource"
	}

	// cb-store
	Key := common.GenResourceKey(nsId, common.StrVNet, content.Id)
	Val, _ := json.Marshal(content)

	//fmt.Println("Key: ", Key)
	//fmt.Println("Val: ", Val)
	err = common.CBStore.Put(Key, string(Val))
	if err != nil {
		common.CBLog.Error(err)
		return content, err
	}

	for _, v := range tempSpiderVPCInfo.SubnetInfoList {
		jsonBody, err := json.Marshal(v)
		if err != nil {
			common.CBLog.Error(err)
		}

		tbSubnetReq := TbSubnetReq{}
		err = json.Unmarshal(jsonBody, &tbSubnetReq)
		if err != nil {
			common.CBLog.Error(err)
		}
		tbSubnetReq.Name = v.IId.NameId

		_, err = CreateSubnet(nsId, content.Id, tbSubnetReq, true)
		if err != nil {
			common.CBLog.Error(err)
		}
	}
	keyValue, err := common.CBStore.Get(Key)
	if err != nil {
		common.CBLog.Error(err)
		err = fmt.Errorf("In CreateVNet(); CBStore.Get() returned an error.")
		common.CBLog.Error(err)
		// return nil, err
	}

	fmt.Println("<" + keyValue.Key + "> \n" + keyValue.Value)
	fmt.Println("===========================")

	result := TbVNetInfo{}
	err = json.Unmarshal([]byte(keyValue.Value), &result)
	if err != nil {
		common.CBLog.Error(err)
	}
	return result, nil
}
