import unittest

from corepulse.faults import SafetyError, plan_pod_crash, run_pod_crash


class FakeKubectl:
    def __init__(self, context="open5gs-lab", owner="open5gs-amf-abc"):
        self.context = context
        self.owner = owner
        self.commands = []
        self.pod_reads = 0

    def run(self, *args, timeout=30):
        self.commands.append(args)
        if args == ("config", "current-context"):
            return self.context
        return "ok"

    def get_json(self, *args):
        self.commands.append(args)
        if args[1] == "deployment/open5gs-amf":
            return {
                "spec": {"replicas": 1, "selector": {"matchLabels": {"app": "amf"}}},
                "status": {"readyReplicas": 1},
            }
        self.pod_reads += 1
        pod_name = "open5gs-amf-abc-123" if self.pod_reads == 1 else "open5gs-amf-def-456"
        return {"items": [{
            "metadata": {
                "name": pod_name,
                "ownerReferences": [{"kind": "ReplicaSet", "name": self.owner if self.pod_reads == 1 else "open5gs-amf-def"}],
            },
            "status": {
                "phase": "Running",
                "conditions": [{"type": "Ready", "status": "True"}],
            },
        }]}


class FaultTests(unittest.TestCase):
    def test_unlisted_target_is_rejected(self):
        with self.assertRaises(SafetyError):
            plan_pod_crash("other")

    def test_wrong_context_never_deletes(self):
        fake = FakeKubectl(context="production")
        with self.assertRaises(SafetyError):
            run_pod_crash(fake, "amf")
        self.assertFalse(any(command[0] == "delete" for command in fake.commands))

    def test_unowned_pod_never_deletes(self):
        fake = FakeKubectl(owner="other-app-abc")
        with self.assertRaises(SafetyError):
            run_pod_crash(fake, "amf")
        self.assertFalse(any(command[0] == "delete" for command in fake.commands))

    def test_crash_waits_for_deletion_and_recovery(self):
        fake = FakeKubectl()
        result = run_pod_crash(fake, "amf")
        self.assertEqual(result["status"], "recovered")
        self.assertEqual(result["replacement_pod"], "open5gs-amf-def-456")
        actions = [command[0] for command in fake.commands]
        self.assertLess(actions.index("delete"), actions.index("wait"))
        self.assertLess(actions.index("wait"), actions.index("rollout"))
        self.assertIn("--namespace", fake.commands[3])
        self.assertIn("open5gs", fake.commands[3])


if __name__ == "__main__":
    unittest.main()
