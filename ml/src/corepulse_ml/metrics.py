"""Prediction-derived metrics matching the Go reference's abstention and vote rules."""

from collections import Counter, defaultdict


def measure(labels: list[str], predictions: list[dict]) -> dict:
    if not labels or len(labels) != len(set(labels)):
        raise ValueError("metric labels must be nonempty and unique")
    positions = {label: index for index, label in enumerate(labels)}
    confusion = [[0 for _ in labels] for _ in labels]
    support, votes = Counter(), Counter()
    abstained = 0
    for row in predictions:
        truth, predicted = row["truth"], row["predicted"]
        if truth not in positions or predicted and predicted not in positions:
            raise ValueError("prediction uses unsupported truth/class")
        support[truth] += 1
        if predicted == "":
            abstained += 1
        else:
            confusion[positions[truth]][positions[predicted]] += 1
            votes[predicted] += 1
    classes = {}
    for label, index in positions.items():
        true_positive = confusion[index][index]
        precision = true_positive / votes[label] if votes[label] else 0.0
        recall = true_positive / support[label] if support[label] else 0.0
        f1 = 2 * precision * recall / (precision + recall) if precision + recall else 0.0
        classes[label] = {
            "support": support[label],
            "predicted": votes[label],
            "precision": precision,
            "recall": recall,
            "f1": f1,
        }
    total = len(predictions)
    predicted_count = total - abstained
    correct = sum(confusion[i][i] for i in range(len(labels)))
    return {
        "labels": labels,
        "confusion": confusion,
        "classes": classes,
        "total": total,
        "predicted": predicted_count,
        "abstained": abstained,
        "coverage": predicted_count / total if total else 0.0,
        "accuracy_on_predicted": correct / predicted_count if predicted_count else 0.0,
        "macro_f1": sum(c["f1"] for c in classes.values()) / len(labels),
    }


def by_run(labels: list[str], predictions: list[dict]) -> list[dict]:
    groups = defaultdict(list)
    for row in predictions:
        groups[row["run_id"]].append(row)
    result = []
    for run, rows in sorted(groups.items()):
        if len({row["truth"] for row in rows}) != 1:
            raise ValueError("conflicting within-run truths")
        votes = Counter(row["predicted"] for row in rows if row["predicted"])
        predicted = max(labels, key=lambda label: votes[label]) if votes else ""
        result.append({"run_id": run, "truth": rows[0]["truth"], "predicted": predicted})
    return result


def model_report(task: str, labels: list[str], predictions: list[dict], **metadata) -> dict:
    return {
        "task": task,
        "predictions": predictions,
        "window_metrics": measure(labels, predictions),
        "run_metrics": measure(labels, by_run(labels, predictions)),
        **metadata,
    }
