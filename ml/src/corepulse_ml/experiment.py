"""Run a declared synthetic offline experiment from a strict YAML configuration."""

from __future__ import annotations

import argparse
import hashlib
import re
from dataclasses import asdict, dataclass
from pathlib import Path

import yaml

from corepulse_ml.autoencoder import read_options as read_autoencoder_options
from corepulse_ml.autoencoder import train_autoencoder
from corepulse_ml.data import RESOURCE_FEATURES, load_windows, strict_json, validate_features
from corepulse_ml.export import artifact_for, ensure_artifact_destination, write_artifact
from corepulse_ml.mlp import read_options as read_mlp_options
from corepulse_ml.mlp import train_mlp
from corepulse_ml.reporting import compose_report, validate_output_path, write_report
from corepulse_ml.validation import grouped_folds


class UniqueSafeLoader(yaml.SafeLoader):
    pass


def _mapping(loader, node, deep=False):
    result = {}
    for key_node, value_node in node.value:
        key = loader.construct_object(key_node, deep=deep)
        if not isinstance(key, str) or key in result:
            raise ValueError("YAML keys must be unique strings")
        result[key] = loader.construct_object(value_node, deep=deep)
    return result


UniqueSafeLoader.add_constructor(yaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG, _mapping)


@dataclass(frozen=True)
class Experiment:
    name: str
    windows: Path
    split: Path
    references: Path
    output: Path
    seeds: tuple[int, ...]
    inner_folds: int
    inner_seed: int
    features: tuple[str, ...]
    methods: tuple[dict, ...]
    config_sha256: str


def read_experiment(path: str | Path) -> Experiment:
    raw = Path(path).read_bytes()
    config = yaml.load(raw, Loader=UniqueSafeLoader)
    required = {
        "schema_version",
        "name",
        "synthetic",
        "dataset",
        "references",
        "output",
        "seeds",
        "inner_validation",
        "methods",
    }
    if (
        not isinstance(config, dict)
        or not required <= config.keys()
        or config.keys() - required - {"features"}
    ):
        raise ValueError("missing/unknown experiment YAML keys")
    if type(config["schema_version"]) is not int or config["schema_version"] != 1:
        raise ValueError("unsupported experiment schema")
    if config["synthetic"] is not True:
        raise ValueError("only explicitly synthetic development experiments are enabled")
    name = config["name"]
    if not isinstance(name, str) or not re.fullmatch(r"[a-z][a-z0-9-]{0,63}", name):
        raise ValueError("invalid experiment name")
    dataset, inner = config["dataset"], config["inner_validation"]
    if not isinstance(dataset, dict) or dataset.keys() != {"windows", "split"}:
        raise ValueError("dataset requires exact windows/split paths")
    if not isinstance(inner, dict) or inner.keys() != {"folds", "seed"}:
        raise ValueError("inner validation requires exact folds/seed")
    if type(inner["folds"]) is not int or not 2 <= inner["folds"] <= 32:
        raise ValueError("inner folds must be between 2 and 32")
    if type(inner["seed"]) is not int or not -(2**63) <= inner["seed"] < 2**63:
        raise ValueError("invalid inner seed")
    seeds = config["seeds"]
    if (
        not isinstance(seeds, list)
        or not seeds
        or len(seeds) > 32
        or any(type(s) is not int or not 0 <= s < 2**63 for s in seeds)
        or len(set(seeds)) != len(seeds)
    ):
        raise ValueError("model seeds must be unique nonnegative int64 values (maximum 32)")
    methods = config["methods"]
    if not isinstance(methods, list) or any(not isinstance(method, dict) for method in methods):
        raise ValueError("methods must be a list of configurations")
    kinds = set()
    for method in methods:
        kind = method.get("kind")
        if kind not in ("mlp", "autoencoder") or kind in kinds:
            raise ValueError("unsupported or duplicate experiment method kind")
        (read_mlp_options if kind == "mlp" else read_autoencoder_options)(method)
        kinds.add(kind)
    paths = [dataset["windows"], dataset["split"], config["references"], config["output"]]
    if any(not isinstance(value, str) or not value.strip() for value in paths):
        raise ValueError(
            "experiment paths must be nonempty strings relative to the working directory"
        )
    return Experiment(
        name,
        *(Path(value) for value in paths),
        tuple(seeds),
        inner["folds"],
        inner["seed"],
        validate_features(config.get("features", RESOURCE_FEATURES)),
        tuple(methods),
        hashlib.sha256(raw).hexdigest(),
    )


def run_experiment(path: str | Path, *, export_dir: str | Path | None = None) -> dict:
    config = read_experiment(path)
    sources = [path, config.windows, config.split, config.references]
    outputs = [config.output]
    if export_dir is not None:
        export_dir = ensure_artifact_destination(export_dir)
        if export_dir.exists() and not export_dir.is_dir():
            raise ValueError("export directory is an existing file")
        outputs.extend(
            ensure_artifact_destination(export_dir / f"{method['kind']}_seed_{seed}.json")
            for method in config.methods
            for seed in config.seeds
        )
    for index, output in enumerate(outputs):
        validate_output_path(output, [*sources, *outputs[:index], *outputs[index + 1 :]])
    dataset = load_windows(config.windows, config.split, config.features)
    folds = grouped_folds(dataset, config.inner_folds, config.inner_seed)
    reference = strict_json(config.references.read_bytes())
    metadata = {
        "name": config.name,
        "synthetic": True,
        "config_sha256": config.config_sha256,
        "split_sha256": dataset.split_sha256,
        "seeds": list(config.seeds),
        "inner_validation": [asdict(fold) for fold in folds],
        "method_configurations": list(config.methods),
    }
    # Verify reference identities/metrics against these exact input files
    # before doing any training. This checks consistency, not origin/authenticity.
    compose_report(dataset, reference, {}, metadata)
    methods = {}
    artifacts = {}
    for method in config.methods:
        for seed in config.seeds:
            trainer = train_mlp if method["kind"] == "mlp" else train_autoencoder
            trained = trainer(dataset, folds, method, seed)
            name = f"{method['kind']}_seed_{seed}"
            methods[name] = trained.report
            if export_dir is not None:
                artifacts[name] = artifact_for(trained, dataset, synthetic=True)
    report = compose_report(dataset, reference, methods, metadata)
    if export_dir is not None:
        sources = [path, config.windows, config.split, config.references, config.output]
        descriptors = {}
        for name, artifact in artifacts.items():
            filename = f"{name}.json"
            digest = write_artifact(export_dir / filename, artifact, sources)
            descriptors[name] = {"filename": filename, "sha256": digest, "schema_version": 1}
        report["experiment"]["artifacts"] = descriptors
    write_report(config.output, report, [path, config.windows, config.split, config.references])
    return report


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("configuration", type=Path)
    parser.add_argument(
        "--export-dir", type=Path, help="ignored/local directory for versioned JSON coefficients"
    )
    args = parser.parse_args()
    try:
        report = run_experiment(args.configuration, export_dir=args.export_dir)
    except (ValueError, OSError, yaml.YAMLError) as exc:
        parser.exit(1, f"Experiment refused: {exc}\n")
    print(
        f"SYNTHETIC ONLY: {report['test_windows']} invented held-out windows; "
        f"{len(report['models'])} methods. Not a real-lab score."
    )


if __name__ == "__main__":
    main()
