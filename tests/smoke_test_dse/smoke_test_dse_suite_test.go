// Copyright DataStax, Inc.
// Please see the included license file for details.

package smoke_test_dse

import (
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	"github.com/k8ssandra/cass-operator/tests/kustomize"
	ginkgo_util "github.com/k8ssandra/cass-operator/tests/util/ginkgo"
	"github.com/k8ssandra/cass-operator/tests/util/kubectl"
)

var (
	testName   = "Smoke test of basic functionality for one-node DSE cluster."
	namespace  = "test-smoke-test-dse"
	dcName     = "dc2"
	dcYaml     = "../testdata/smoke-test-dse.yaml"
	dcResource = fmt.Sprintf("CassandraDatacenter/%s", dcName)
	dcLabel    = fmt.Sprintf("cassandra.datastax.com/datacenter=%s", dcName)
	ns         = ginkgo_util.NewWrapper(testName, namespace)
)

func TestLifecycle(t *testing.T) {
	AfterSuite(func() {
		logPath := fmt.Sprintf("%s/aftersuite", ns.LogDir)
		err := kubectl.DumpAllLogs(logPath).ExecV()
		if err != nil {
			t.Logf("Failed to dump all the logs: %v", err)
		}

		fmt.Printf("\n\tPost-run logs dumped at: %s\n\n", logPath)
		ns.Terminate()
		err = kustomize.Undeploy(namespace)
		if err != nil {
			t.Logf("Failed to undeploy cass-operator: %v", err)
		}
	})

	RegisterFailHandler(Fail)
	RunSpecs(t, testName)
}

var _ = Describe(testName, func() {
	Context("when in a new cluster", func() {
		Specify("the operator can stand up a one node cluster", func() {
			By("deploy cass-operator with kustomize")
			err := kustomize.Deploy(namespace)
			Expect(err).ToNot(HaveOccurred())

			step := "waiting for the operator to become ready"
			json := "jsonpath={.items[0].status.containerStatuses[0].ready}"
			k := kubectl.Get("pods").
				WithLabel("name=cass-operator").
				WithFlag("field-selector", "status.phase=Running").
				FormatOutput(json)
			ns.WaitForOutputAndLog(step, k, "true", 360)

			step = "creating a datacenter resource with 1 rack/1 node"
			k = kubectl.ApplyFiles(dcYaml)
			ns.ExecAndLog(step, k)

			ns.WaitForDatacenterReady(dcName)
			ns.ExpectDoneReconciling(dcName)

			step = "scale up to 2 nodes"
			json = "{\"spec\": {\"size\": 2}}"
			k = kubectl.PatchMerge(dcResource, json)
			ns.ExecAndLog(step, k)

			ns.WaitForDatacenterCondition(dcName, "ScalingUp", string(corev1.ConditionTrue))
			ns.WaitForDatacenterOperatorProgress(dcName, "Updating", 60)
			ns.WaitForDatacenterCondition(dcName, "ScalingUp", string(corev1.ConditionFalse))

			// Ensure that when 'ScaleUp' becomes 'false' that our pods are in fact up and running
			Expect(len(ns.GetDatacenterReadyPodNames(dcName))).To(Equal(2))

			ns.WaitForDatacenterReady(dcName)

			step = "deleting the dc"
			k = kubectl.DeleteFromFiles(dcYaml)
			ns.ExecAndLog(step, k)

			step = "checking that the dc no longer exists"
			json = "jsonpath={.items}"
			k = kubectl.Get("CassandraDatacenter").
				WithLabel(dcLabel).
				FormatOutput(json)
			ns.WaitForOutputAndLog(step, k, "[]", 600)

			step = "checking that no dc pods remain"
			json = "jsonpath={.items}"
			k = kubectl.Get("pods").
				WithLabel(dcLabel).
				FormatOutput(json)
			ns.WaitForOutputAndLog(step, k, "[]", 600)

			step = "checking that no dc services remain"
			json = "jsonpath={.items}"
			k = kubectl.Get("services").
				WithLabel(dcLabel).
				FormatOutput(json)
			ns.WaitForOutputAndLog(step, k, "[]", 600)

			step = "checking that no dc stateful sets remain"
			json = "jsonpath={.items}"
			k = kubectl.Get("statefulsets").
				WithLabel(dcLabel).
				FormatOutput(json)
			ns.WaitForOutputAndLog(step, k, "[]", 600)
		})
	})
})
