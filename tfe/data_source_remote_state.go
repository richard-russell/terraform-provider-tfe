package tfe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	//"math/big"
	"os"

	tfe "github.com/hashicorp/go-tfe"
	"github.com/hashicorp/terraform-plugin-go/tfprotov5"
	"github.com/hashicorp/terraform-plugin-go/tfprotov5/tftypes"
	"github.com/zclconf/go-cty/cty"
	//gocty "github.com/zclconf/go-cty/cty/gocty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

type dataSourceRemoteState struct {
	provider *providerServer
}

var stderr *os.File

func init() {
	stderr = os.Stderr
}

func newDataSourceRemoteState(p *providerServer) tfprotov5.DataSourceServer {
	return dataSourceRemoteState{
		provider: p,
	}
}

func (d dataSourceRemoteState) ReadDataSource(ctx context.Context, req *tfprotov5.ReadDataSourceRequest) (*tfprotov5.ReadDataSourceResponse, error) {
	resp := &tfprotov5.ReadDataSourceResponse{
		Diagnostics: []*tfprotov5.Diagnostic{},
	}
	client, err := getClient(d.provider.meta.hostname, d.provider.meta.token, false)
	if err != nil {
		return nil, err
	}

	orgName, wsName, err := readConfigValues(req)
	if err != nil {
		resp.Diagnostics = append(resp.Diagnostics, &tfprotov5.Diagnostic{
			Severity: tfprotov5.DiagnosticSeverityError,
			Summary:  "Error retrieving values from the config",
			Detail:   fmt.Sprintf("Error retrieving values from the config: %v", err),
		})
		return resp, nil
	}

	remoteStateOutput, err := d.readRemoteStateOutput(ctx, client, orgName, wsName)
	if err != nil {
		resp.Diagnostics = append(resp.Diagnostics, &tfprotov5.Diagnostic{
			Severity: tfprotov5.DiagnosticSeverityError,
			Summary:  "Error reading remote state output",
			Detail:   fmt.Sprintf("Error reading remote state output: %v", err),
		})
		return resp, nil
	}

	tftypesValues, stateTypes, err := parseStateOutput(remoteStateOutput)
	if err != nil {
		resp.Diagnostics = append(resp.Diagnostics, &tfprotov5.Diagnostic{
			Severity: tfprotov5.DiagnosticSeverityError,
			Summary:  "Error parsing remote state output",
			Detail:   fmt.Sprintf("Error parsing remote state output: %v", err),
		})
		return resp, nil
	}

	state, err := tfprotov5.NewDynamicValue(tftypes.Object{
		AttributeTypes: map[string]tftypes.Type{
			"workspace":    tftypes.String,
			"organization": tftypes.String,
			"values":       tftypes.DynamicPseudoType,
		},
	}, tftypes.NewValue(tftypes.Object{
		AttributeTypes: map[string]tftypes.Type{
			"workspace":    tftypes.String,
			"organization": tftypes.String,
			"values":       tftypes.Object{AttributeTypes: stateTypes},
		},
	}, map[string]tftypes.Value{
		"workspace":    tftypes.NewValue(tftypes.String, wsName),
		"organization": tftypes.NewValue(tftypes.String, orgName),
		"values":       tftypes.NewValue(tftypes.Object{AttributeTypes: stateTypes}, tftypesValues),
	}))

	if err != nil {
		return &tfprotov5.ReadDataSourceResponse{
			Diagnostics: []*tfprotov5.Diagnostic{
				{
					Severity: tfprotov5.DiagnosticSeverityError,
					Summary:  "Error encoding state",
					Detail:   fmt.Sprintf("Error encoding state: %s", err.Error()),
				},
			},
		}, nil
	}
	return &tfprotov5.ReadDataSourceResponse{
		State: &state,
	}, nil
}

func (d dataSourceRemoteState) ValidateDataSourceConfig(ctx context.Context, req *tfprotov5.ValidateDataSourceConfigRequest) (*tfprotov5.ValidateDataSourceConfigResponse, error) {
	return &tfprotov5.ValidateDataSourceConfigResponse{}, nil
}

func readConfigValues(req *tfprotov5.ReadDataSourceRequest) (string, string, error) {
	var orgName string
	var wsName string
	var err error

	config := req.Config
	val, err := config.Unmarshal(tftypes.Object{
		AttributeTypes: map[string]tftypes.Type{
			"workspace":    tftypes.String,
			"organization": tftypes.String,
			"values":       tftypes.String,
		}})
	if err != nil {
		return orgName, wsName, fmt.Errorf("Error unmarshalling config: %v", err)
	}

	var valMap map[string]tftypes.Value
	err = val.As(&valMap)
	if err != nil {
		return orgName, wsName, fmt.Errorf("Error assigning configuration attributes to map: %v", err)
	}

	err = valMap["organization"].As(&orgName)
	if err != nil {
		return orgName, wsName, fmt.Errorf("Error assigning 'organization' value to string: %v", err)
	}
	err = valMap["workspace"].As(&wsName)
	if err != nil {
		return orgName, wsName, fmt.Errorf("Error assigning 'workspace' value to string: %v", err)
	}

	return orgName, wsName, nil
}

type rawRemoteState struct {
	RootOutputs map[string]rawOutputState `json:"outputs"`
}

type rawOutputState struct {
	ValueRaw     json.RawMessage `json:"value"`
	ValueTypeRaw json.RawMessage `json:"type"`
	Sensitive    bool            `json:"sensitive,omitempty"`
}

type OutputData struct {
	Addr      outputValue
	Value     cty.Value
	Sensitive bool
}

type outputValue struct {
	Value outputValueName
}

type outputValueName struct {
	Name string
}

type remoteStateData struct {
	Outputs map[string]*OutputData
}

func (d dataSourceRemoteState) readRemoteStateOutput(ctx context.Context, tfeClient *tfe.Client, orgName, wsName string) (*remoteStateData, error) {
	log.Printf("[DEBUG] Reading the Workspace %s in Organization %s", wsName, orgName)
	ws, err := tfeClient.Workspaces.Read(ctx, orgName, wsName)
	if err != nil {
		return nil, fmt.Errorf("Error reading workspace: %v", err)
	}

	log.Printf("[DEBUG] Reading the current StateVersion for Workspace ID %s.", ws.ID)
	sv, err := tfeClient.StateVersions.Current(ctx, ws.ID)
	if err != nil {
		if err == tfe.ErrResourceNotFound {
			return nil, fmt.Errorf("Current remote state for workspace '%s' not found.", wsName)
		}
		return nil, fmt.Errorf("Could not read the current remote state for workspace '%s' : %v", wsName, err)
	}

	log.Printf("[DEBUG] Downloading State Version")
	stateData, err := tfeClient.StateVersions.Download(ctx, sv.DownloadURL)
	if err != nil {
		return nil, fmt.Errorf("Error downloading remote state: %v", err)
	}

	log.Printf("[DEBUG] Unmarshalling remote state output")
	read := bytes.NewReader(stateData)
	src, err := ioutil.ReadAll(read)
	if err != nil {
		return nil, fmt.Errorf("Could not read state data: %v", err)
	}
	rrs := &rawRemoteState{}
	err = json.Unmarshal(src, rrs)
	if err != nil {
		return nil, fmt.Errorf("Could not unmarshal state data: %v", err)
	}

	fov := &remoteStateData{
		Outputs: map[string]*OutputData{},
	}
	for name, fos := range rrs.RootOutputs {
		os := &OutputData{
			Addr: outputValue{
				Value: outputValueName{
					Name: name,
				},
			},
		}
		os.Sensitive = fos.Sensitive

		ty, err := ctyjson.UnmarshalType([]byte(fos.ValueTypeRaw))
		if err != nil {
			return nil, fmt.Errorf("Could not unmarshal type: %v", err)
		}

		val, err := ctyjson.Unmarshal([]byte(fos.ValueRaw), ty)
		if err != nil {
			return nil, fmt.Errorf("Could not unmarshal value: %v", err)
		}

		os.Value = val
		fov.Outputs[name] = os
	}

	return fov, nil
}

func parseStateOutput(stateOutput *remoteStateData) (map[string]tftypes.Value, map[string]tftypes.Type, error) {
	tftypesValues := map[string]tftypes.Value{}
	stateTypes := map[string]tftypes.Type{}

	for name, outputValues := range stateOutput.Outputs {
		marshData, err := outputValues.Value.Type().MarshalJSON()
		if err != nil {
			return nil, nil, fmt.Errorf("Could not marshall output type: %v", err)
		}
		tfType, err := tftypes.ParseJSONType(marshData)
		if err != nil {
			return nil, nil, fmt.Errorf("Could not parse json type data: %v", err)
		}
		mByte, err := ctyjson.Marshal(outputValues.Value, outputValues.Value.Type())
		if err != nil {
			return nil, nil, fmt.Errorf("Could not marshal output value and output type: %v", err)
		}
		rawState := tfprotov5.RawState{
			JSON: mByte,
		}
		newVal, err := rawState.Unmarshal(tfType)
		if err != nil {
			return nil, nil, fmt.Errorf("Could not unmarshal tftype into value: %v", err)
		}
		tftypesValues[name] = newVal
		stateTypes[name] = tfType
	}

	return tftypesValues, stateTypes, nil
}
