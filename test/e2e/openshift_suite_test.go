/*
Copyright 2024 The Kubeflow authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e_test

import (
	"context"
	"fmt"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/repo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// ============================================================================
// OPENSHIFT TEST SUITE CONFIGURATION
// ============================================================================
//
// This test suite installs the Spark operator from a REMOTE Helm repository
// (https://shruthis4.github.io/spark-operator) instead of the local chart directory.
//
// Key differences from the standard e2e tests:
// 1. Uses Helm repo add + install (remote) instead of local chart path
// 2. Verifies fsGroup is NOT 185 (OpenShift security requirement)
// 3. Runs the docling-spark-app.yaml workload
// ============================================================================

const (
	// OpenShift-specific constants
	OpenShiftReleaseName      = "spark-operator-openshift"
	OpenShiftReleaseNamespace = "spark-operator-openshift"

	// Your custom Helm repository
	OpenShiftHelmRepoName = "shruthis4-spark-operator"
	OpenShiftHelmRepoURL  = "https://shruthis4.github.io/spark-operator"

	// Chart name in the repository
	OpenShiftChartName = "spark-operator"

	// Namespace for docling spark app
	DoclingNamespace = "docling-spark"
)

// setupOpenShiftTestSuite is called before OpenShift-specific tests run.
// It installs the Spark operator from the remote Helm repository.
func setupOpenShiftTestSuite() {
	By("Setting up OpenShift test environment")

	// Step 1: Create the release namespace
	By("Creating OpenShift release namespace: " + OpenShiftReleaseNamespace)
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: OpenShiftReleaseNamespace}}
	err := k8sClient.Create(context.TODO(), namespace)
	if err != nil {
		logf.Log.Info("Namespace may already exist", "error", err)
	}

	// Step 2: Create namespace for docling spark app
	By("Creating docling namespace: " + DoclingNamespace)
	doclingNs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: DoclingNamespace}}
	err = k8sClient.Create(context.TODO(), doclingNs)
	if err != nil {
		logf.Log.Info("Docling namespace may already exist", "error", err)
	}

	// Step 3: Create service account for spark driver in docling namespace
	By("Creating spark-driver service account")
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "spark-driver",
			Namespace: DoclingNamespace,
		},
	}
	err = k8sClient.Create(context.TODO(), sa)
	if err != nil {
		logf.Log.Info("Service account may already exist", "error", err)
	}

	// Step 4: Add the Helm repository
	By("Adding Helm repository: " + OpenShiftHelmRepoURL + " as " + OpenShiftHelmRepoName)
	addHelmRepo()

	// Step 5: Install the chart from the remote repository
	By("Installing Spark operator from remote Helm repository")
	installChartFromRepo()

	// Step 6: Wait for webhooks to be ready
	By("Waiting for webhooks to be ready")
	mutatingWebhookKey := types.NamespacedName{Name: MutatingWebhookName}
	validatingWebhookKey := types.NamespacedName{Name: ValidatingWebhookName}
	Expect(waitForMutatingWebhookReady(context.Background(), mutatingWebhookKey)).NotTo(HaveOccurred())
	Expect(waitForValidatingWebhookReady(context.Background(), validatingWebhookKey)).NotTo(HaveOccurred())

	// Give webhooks a moment to fully initialize
	time.Sleep(10 * time.Second)

	By("OpenShift test environment setup complete")
}

// teardownOpenShiftTestSuite cleans up after OpenShift tests.
func teardownOpenShiftTestSuite() {
	By("Tearing down OpenShift test environment")

	// Uninstall the Helm release
	By("Uninstalling Spark operator Helm release")
	uninstallOpenShiftChart()

	// Delete namespaces
	By("Deleting OpenShift release namespace")
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: OpenShiftReleaseNamespace}}
	_ = k8sClient.Delete(context.TODO(), namespace)

	By("Deleting docling namespace")
	doclingNs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: DoclingNamespace}}
	_ = k8sClient.Delete(context.TODO(), doclingNs)

	By("OpenShift test environment teardown complete")
}

// addHelmRepo adds the custom Helm repository to the local Helm configuration.
// This is equivalent to running: helm repo add shruthis4-spark-operator https://shruthis4.github.io
func addHelmRepo() {
	// Create a new repository entry
	repoEntry := &repo.Entry{
		Name: OpenShiftHelmRepoName,
		URL:  OpenShiftHelmRepoURL,
	}

	// Get Helm settings
	settings := cli.New()

	// Create repository file if it doesn't exist
	repoFile := settings.RepositoryConfig
	r, err := repo.NewChartRepository(repoEntry, getter.All(settings))
	Expect(err).NotTo(HaveOccurred())

	// Download the repository index
	_, err = r.DownloadIndexFile()
	Expect(err).NotTo(HaveOccurred(), "Failed to download Helm repo index. Make sure %s is accessible", OpenShiftHelmRepoURL)

	// Load existing repo file or create new one
	var repoFileObj *repo.File
	if _, err := os.Stat(repoFile); os.IsNotExist(err) {
		repoFileObj = repo.NewFile()
	} else {
		repoFileObj, err = repo.LoadFile(repoFile)
		Expect(err).NotTo(HaveOccurred())
	}

	// Update or add the repo
	repoFileObj.Update(repoEntry)

	// Write the updated repo file
	Expect(repoFileObj.WriteFile(repoFile, 0644)).NotTo(HaveOccurred())

	logf.Log.Info("Successfully added Helm repository", "name", OpenShiftHelmRepoName, "url", OpenShiftHelmRepoURL)
}

// installChartFromRepo installs the Spark operator chart from the remote repository.
// This is equivalent to: helm install spark-operator-openshift shruthis4-spark-operator/spark-operator
func installChartFromRepo() {
	settings := cli.New()
	settings.SetNamespace(OpenShiftReleaseNamespace)

	// Initialize action configuration
	actionConfig := &action.Configuration{}
	Expect(actionConfig.Init(settings.RESTClientGetter(), settings.Namespace(), os.Getenv("HELM_DRIVER"), func(format string, v ...interface{}) {
		logf.Log.Info(fmt.Sprintf(format, v...))
	})).NotTo(HaveOccurred())

	// Create install action
	client := action.NewInstall(actionConfig)
	client.ReleaseName = OpenShiftReleaseName
	client.Namespace = OpenShiftReleaseNamespace
	client.Wait = true
	client.Timeout = WaitTimeout
	client.CreateNamespace = true

	// Locate the chart in the repository
	// Format: repoName/chartName (e.g., "shruthis4-spark-operator/spark-operator")
	chartRef := fmt.Sprintf("%s/%s", OpenShiftHelmRepoName, OpenShiftChartName)

	// Locate and download the chart
	chartPath, err := client.ChartPathOptions.LocateChart(chartRef, settings)
	Expect(err).NotTo(HaveOccurred(), "Failed to locate chart: %s", chartRef)

	// Load the chart from the downloaded path
	chartRequested, err := loader.Load(chartPath)
	Expect(err).NotTo(HaveOccurred(), "Failed to load chart from: %s", chartPath)

	// Install with empty values (use chart defaults)
	// You can customize values here if needed
	vals := map[string]interface{}{}

	release, err := client.Run(chartRequested, vals)
	Expect(err).NotTo(HaveOccurred(), "Failed to install Helm chart")
	Expect(release).NotTo(BeNil())

	logf.Log.Info("Successfully installed Spark operator from remote repo",
		"release", OpenShiftReleaseName,
		"namespace", OpenShiftReleaseNamespace,
		"chartPath", chartPath)
}

// uninstallOpenShiftChart removes the Helm release.
func uninstallOpenShiftChart() {
	settings := cli.New()
	settings.SetNamespace(OpenShiftReleaseNamespace)

	actionConfig := &action.Configuration{}
	err := actionConfig.Init(settings.RESTClientGetter(), settings.Namespace(), os.Getenv("HELM_DRIVER"), func(format string, v ...interface{}) {
		logf.Log.Info(fmt.Sprintf(format, v...))
	})
	if err != nil {
		logf.Log.Error(err, "Failed to initialize action config for uninstall")
		return
	}

	uninstallAction := action.NewUninstall(actionConfig)
	uninstallAction.Wait = true
	uninstallAction.Timeout = WaitTimeout

	_, err = uninstallAction.Run(OpenShiftReleaseName)
	if err != nil {
		logf.Log.Error(err, "Failed to uninstall Helm release")
	}
}
