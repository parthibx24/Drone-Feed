"""Normalises model class names to the labels and groups in docs/09-ai.md."""

# model class name (lower case) -> (label, group)
_MAP = {
    "person": ("person", "person"),
    "pedestrian": ("person", "person"),
    "people": ("person", "person"),
    "car": ("car", "vehicle"),
    "van": ("van", "vehicle"),
    "truck": ("truck", "vehicle"),
    "bus": ("bus", "vehicle"),
    "bicycle": ("bike", "vehicle"),
    "motor": ("bike", "vehicle"),
    "motorcycle": ("bike", "vehicle"),
    "bicycle_motor": ("bike", "vehicle"),
    "tricycle": ("three_wheeler", "vehicle"),
    "awning-tricycle": ("three_wheeler", "vehicle"),
    "other_vehicle": ("vehicle", "vehicle"),
    "othervehicle": ("vehicle", "vehicle"),
    "boat_ship": ("boat", "other"),
    "boat": ("boat", "other"),
    "aircraft": ("aircraft", "other"),
}


def normalise(name: str) -> tuple[str, str]:
    key = name.strip().lower()
    if key in _MAP:
        return _MAP[key]
    return key.replace(" ", "_") or "object", "other"
