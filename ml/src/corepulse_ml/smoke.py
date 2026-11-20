"""A small invented CPU tensor calculation, not a lab result or trained detector."""

import json

import torch


def smoke() -> dict:
    if torch.version.cuda is not None:
        raise RuntimeError("CorePulse development requires the locked CPU-only PyTorch build")
    x = torch.tensor([[0.0, 1.0], [1.0, 0.0]], dtype=torch.float32, device="cpu")
    return {"synthetic": True, "device": str(x.device), "row_sums": x.sum(dim=1).tolist()}


def main() -> None:
    print(json.dumps(smoke(), sort_keys=True))


if __name__ == "__main__":
    main()
