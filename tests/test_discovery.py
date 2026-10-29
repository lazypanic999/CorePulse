import unittest

from corepulse.discovery import analyze_snapshot


class DiscoveryTests(unittest.TestCase):
    def test_inventory_does_not_claim_metrics_are_verified(self):
        snapshot = {"items": [
            {
                "kind": "Deployment",
                "metadata": {"name": "open5gs-amf"},
                "spec": {"replicas": 1},
                "status": {"readyReplicas": 1},
            },
            {
                "kind": "Service",
                "metadata": {
                    "name": "sample-metrics",
                    "annotations": {
                        "prometheus.io/scrape": "true",
                        "prometheus.io/port": "9090",
                    },
                },
                "spec": {"ports": [{"port": 9090}]},
            },
        ]}
        result = analyze_snapshot(snapshot, "open5gs-lab")
        self.assertTrue(result["expected_deployments"]["open5gs-amf"])
        self.assertFalse(result["expected_deployments"]["open5gs-upf"])
        self.assertEqual(result["metric_names_verified"], [])
        self.assertEqual(result["prometheus_annotation_candidates"][0]["port"], "9090")
        self.assertFalse(result["prometheus_annotation_candidates"][0]["verified"])

    def test_invalid_list_is_rejected(self):
        with self.assertRaises(ValueError):
            analyze_snapshot({"items": "wrong"}, "open5gs-lab")


if __name__ == "__main__":
    unittest.main()
