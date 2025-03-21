// Copyright DataStax, Inc.
// Please see the included license file for details.

package tolerations

import (
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/k8ssandra/cass-operator/tests/kustomize"
	ginkgo_util "github.com/k8ssandra/cass-operator/tests/util/ginkgo"
	"github.com/k8ssandra/cass-operator/tests/util/kubectl"
)

var (
	testName    = "PSP EMM"
	opNamespace = "test-psp-emm"
	dc1Name     = "dc1"
	// This scenario requires RF greater than 2
	dc1Yaml   = "../testdata/psp-emm-dc.yaml"
	pod1Name  = "cluster1-dc1-r1-sts-0"
	nodeCount = 3
	ns        = ginkgo_util.NewWrapper(testName, opNamespace)
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
		err = kustomize.Undeploy(opNamespace)
		if err != nil {
			t.Logf("Failed to undeploy cass-operator: %v", err)
		}
	})

	RegisterFailHandler(Fail)
	RunSpecs(t, testName)
}

var _ = Describe(testName, func() {
	Context("when in a new cluster", func() {
		Specify("when a node has an evacuate data taint, deletes pods and PVCs on that node", func() {
			By("deploy cass-operator with kustomize")
			err := kustomize.Deploy(opNamespace)
			Expect(err).ToNot(HaveOccurred())

			// step := "setting up cass-operator resources via helm chart"
			// ns.HelmInstallWithPSPEnabled("../../charts/cass-operator-chart")

			ns.WaitForOperatorReady()

			step := "creating first datacenter resource"
			k := kubectl.ApplyFiles(dc1Yaml)
			ns.ExecAndLog(step, k)

			ns.WaitForDatacenterReady(dc1Name)

			k = kubectl.GetNodes()
			nodes, _, err := ns.ExecVCapture(k)
			if err != nil {
				panic(err)
			}

			k = kubectl.Label(
				nodes,
				"kubernetes.io/role",
				"agent",
			)

			err = ns.ExecV(k)
			if err != nil {
				panic(err)
			}

			// Cleanup: Remove the label
			defer func() {
				k := kubectl.Label(
					nodes,
					"kubernetes.io/role",
					"",
				)
				k.ExecVPanic()
			}()

			// Add a taint to the node for the first pod

			k = kubectl.GetNodeNameForPod(pod1Name)
			node1Name, _, err := ns.ExecVCapture(k)
			if err != nil {
				panic(err)
			}

			// Cleanup: Remove the taint
			defer func() {
				k := kubectl.Taint(
					node1Name,
					"node.vmware.com/drain",
					"",
					"NoSchedule-")
				k.ExecVPanic()
			}()

			step = fmt.Sprintf("tainting node: %s", node1Name)
			k = kubectl.Taint(
				node1Name,
				"node.vmware.com/drain",
				"drain",
				"NoSchedule")
			ns.ExecAndLog(step, k)

			// Wait for a pod to no longer be ready

			i := 1
			for i < 300 {
				time.Sleep(1 * time.Second)
				i += 1

				names := ns.GetDatacenterReadyPodNames(dc1Name)
				if len(names) < nodeCount {
					break
				}
			}

			ns.WaitForDatacenterReadyPodCount(dc1Name, 2)

			// In my environment, I have to add a wait here

			time.Sleep(1 * time.Minute)

			// Wait for the cluster to heal itself

			ns.WaitForDatacenterReady(dc1Name)

			// Make sure things look right in nodetool
			step = "verify in nodetool that we still have the right number of cassandra nodes"
			By(step)
			podNames := ns.GetDatacenterReadyPodNames(dc1Name)
			for _, podName := range podNames {
				nodeInfos := ns.RetrieveStatusFromNodetool(podName)
				Expect(len(nodeInfos)).To(Equal(nodeCount), "Expect nodetool to return info on exactly %d nodes", nodeCount)
				for _, nodeInfo := range nodeInfos {
					Expect(nodeInfo.Status).To(Equal("up"), "Expected all nodes to be up, but node %s was down", nodeInfo.HostId)
					Expect(nodeInfo.State).To(Equal("normal"), "Expected all nodes to have a state of normal, but node %s was %s", nodeInfo.HostId, nodeInfo.State)
				}
			}
		})
	})
})
