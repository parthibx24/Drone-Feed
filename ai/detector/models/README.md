# Detector models

Both are Ultralytics YOLO11n detectors exported to ONNX (input 1×3×640×640, output 1×(4+classes)×8400, no NMS in the graph). The detector reads the class names from the file's metadata, so any Ultralytics-format ONNX model can replace these without code changes.

| File | Classes | Trained on | Source | SHA-256 |
|---|---|---|---|---|
| `visible.onnx` | person, car, truck, bus, van, bicycle_motor, boat_ship, aircraft | VisDrone (drone footage) + DOTA (satellite tiles) | `aero_fp32.onnx` from [pavanyadava07/aeroedge-yolo11n](https://huggingface.co/pavanyadava07/aeroedge-yolo11n), unmodified. Published: mAP50 0.569 at 640 px. | `b50972cf849fd58d671273e8c3e8002f682acb028422a60f2a3ea2a4e6e482c5` |
| `thermal.onnx` | person, car, bicycle, other_vehicle | [HIT-UAV](https://github.com/suojiashun/HIT-UAV-Infrared-Thermal-Dataset) (drone thermal footage, 60–130 m, day and night) | Trained for StarWatch with [`ai/training/train_thermal.py`](../../training/train_thermal.py): YOLO11n from COCO weights, 25 epochs at 640 px. Test split: mAP50 0.883, person precision/recall 0.88/0.88 at 0.35. | `76bf1cd3196fb185268129ec59fe95aec611d2741ab3dbb9aecf23280c1ef050` |

## Licences

- **Weights:** both derive from Ultralytics YOLO11 and are distributed under **AGPL-3.0**. Running them in a network service you offer to others triggers AGPL's source-sharing terms for that service.
- **Training data:** VisDrone and DOTA allow **academic use only**. HIT-UAV is **CC BY 4.0** (attribution: Suo et al., "HIT-UAV: A high-altitude infrared thermal dataset for Unmanned Aerial Vehicle-based object detection", *Scientific Data* 10, 2023).

For a commercial deployment, take legal advice. The clean route is to train a permissively licensed architecture on data you may use commercially, ideally your own footage from the C12.
