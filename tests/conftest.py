"""Make the repository root importable so `custom_components.eebus` resolves."""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
