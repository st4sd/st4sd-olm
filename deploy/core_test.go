/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

Authors:
  Vassilis Vassiliadis
*/

package deploy

import (
	"path/filepath"
	"testing"

	"go.uber.org/zap/zapcore"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	st4sdv1alpha1 "github.com/st4sd/st4sd-olm/api/v1alpha1"
)

func TestDryRun(t *testing.T) {
	opts := zap.Options{TimeEncoder: zapcore.ISO8601TimeEncoder}
	zlogger := zap.New(zap.UseFlagOptions(&opts))
	log.SetLogger(zlogger)

	x := st4sdv1alpha1.SimulationToolkitSpecSetup{
		RouteDomain:       "test.example.com",
		PVCInstances:      "workflow-instances",
		PVCDatastore:      "datastore-mongodb",
		PVCRuntimeService: "runtime-service",
	}

	pathChart := filepath.Join("..", "st4sd-deployment", "helm-chart")

	err := HelmDeploySimulationToolkit(pathChart, &x, "vv-playground", true)
	if err != nil {
		t.Fatal("Unable to install/upgrade helm due to", err)
	}
}

/* VV: We need certain cluster permissions (and also a cluster) to run this test
func TestExtractClusterDomain(t *testing.T) {
	domain, err := DiscoverDefaultRoute()

	if err != nil {
		t.Fatalf("Failure %s", err)
	}

	fmt.Printf("Domain is %s\n", domain)
}
*/
