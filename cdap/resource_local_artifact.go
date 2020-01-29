// Copyright 2020 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cdap

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
)

func resourceLocalArtifact() *schema.Resource {
	return &schema.Resource{
		Create: resourceLocalArtifactCreate,
		Read:   resourceLocalArtifactRead,
		Delete: resourceLocalArtifactDelete,
		Exists: resourceLocalArtifactExists,

		Schema: map[string]*schema.Schema{
			"name": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The name of the artifact.",
			},
			"namespace": {
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Description: "The name of the namespace in which this resource belongs. If not provided, the default namespace is used.",
				DefaultFunc: func() (interface{}, error) {
					return defaultNamespace, nil
				},
			},
			// Technically, we could omit the version in the API call because CDAP will infer the
			// version from the jar. However, forcing the user to specify the version makes dealing
			// with the resource easier because other API calls require it.
			"version": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The version of the artifact. Must match the version in the JAR manifest.",
			},
			"jar_binary_path": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The path to the JAR binary for the artifact.",
			},
			"json_config_path": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "THe path to the JSON config of the artifact.",
			},
		},
	}
}

func resourceLocalArtifactCreate(d *schema.ResourceData, m interface{}) error {
	// An artifact with the same name and version can be uploaded multiple times
	// without error. Because of this, there is no need to do partial state
	// management to account for the facdt that setting properties may fail
	// because uploading a jar can occur multiple times without error.
	config := m.(*Config)
	data, err := initArtifactData(d)
	if err != nil {
		return err
	}
	addr := urlJoin(config.host, "/v3/namespaces", d.Get("namespace").(string), "/artifacts", data.name)
	if err := uploadJar(config.client, addr, data); err != nil {
		return err
	}
	if err := uploadProps(config.client, addr, data); err != nil {
		return err
	}
	d.SetId(data.name)
	return nil
}

func uploadJar(client *http.Client, addr string, d *artifactData) error {
	req, err := http.NewRequest(http.MethodPost, addr, bytes.NewReader(d.jar))
	if err != nil {
		return err
	}
	req.Header = map[string][]string{}
	req.Header.Add("Artifact-Version", d.version)
	req.Header.Add("Artifact-Extends", strings.Join(d.config.Parents, "/"))
	if _, err := httpCall(client, req); err != nil {
		return err
	}
	return nil
}

func uploadProps(client *http.Client, artifactAddr string, d *artifactData) error {
	addr := urlJoin(artifactAddr, "/versions", d.version, "/properties")
	b, err := json.Marshal(d.config.Properties)
	if err != nil {
		return err
	}
	body := bytes.NewReader(b)
	req, err := http.NewRequest(http.MethodPut, addr, body)
	if err != nil {
		return err
	}
	if _, err := httpCall(client, req); err != nil {
		return err
	}
	return nil
}

type artifactData struct {
	name    string
	version string
	config  *artifactConfig
	jar     []byte
}

func initArtifactData(d *schema.ResourceData) (*artifactData, error) {
	jar, err := ioutil.ReadFile(d.Get("jar_binary_path").(string))
	if err != nil {
		return nil, err
	}
	ac, err := readArtifactConfig(d.Get("json_config_path").(string))
	if err != nil {
		return nil, err
	}
	name := d.Get("name").(string)
	return &artifactData{
		name:    name,
		version: d.Get("version").(string),
		config:  ac,
		jar:     jar,
	}, nil
}

type artifactConfig struct {
	Properties map[string]string `json:"properties"`
	Parents    []string          `json:"parents"`
}

func readArtifactConfig(fileName string) (*artifactConfig, error) {
	b, err := ioutil.ReadFile(fileName)
	if err != nil {
		return nil, err
	}
	var c artifactConfig
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func resourceLocalArtifactRead(d *schema.ResourceData, m interface{}) error {
	return nil
}

func resourceLocalArtifactDelete(d *schema.ResourceData, m interface{}) error {
	config := m.(*Config)
	name := d.Get("name").(string)
	addr := urlJoin(config.host, "/v3/namespaces", d.Get("namespace").(string), "/artifacts", name, "/versions", d.Get("version").(string))

	req, err := http.NewRequest(http.MethodDelete, addr, nil)
	if err != nil {
		return err
	}
	_, err = httpCall(config.client, req)
	return err
}

func resourceLocalArtifactExists(d *schema.ResourceData, m interface{}) (bool, error) {
	config := m.(*Config)
	name := d.Get("name").(string)
	addr := urlJoin(config.host, "/v3/namespaces", d.Get("namespace").(string), "/artifacts")

	req, err := http.NewRequest(http.MethodGet, addr, nil)
	if err != nil {
		return false, err
	}

	b, err := httpCall(config.client, req)
	if err != nil {
		return false, err
	}

	type artifact struct {
		Name string `json:"name"`
	}

	var artifacts []artifact
	if err := json.Unmarshal(b, &artifacts); err != nil {
		return false, err
	}

	for _, a := range artifacts {
		if a.Name == name {
			return true, nil
		}
	}
	return false, nil
}
