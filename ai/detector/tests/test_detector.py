import os
import unittest

import numpy as np

from detector.labels import normalise
from detector.model import Detection, nms, parse_names
from detector.tracker import Tracker, centre_distance, iou


def det(x, y, w=0.02, h=0.04, label="person", group="person", conf=0.8):
    return Detection(label, group, conf, (x, y, w, h))


class TestLabels(unittest.TestCase):
    def test_normalise(self):
        self.assertEqual(normalise("Person"), ("person", "person"))
        self.assertEqual(normalise("bicycle_motor"), ("bike", "vehicle"))
        self.assertEqual(normalise("other_vehicle"), ("vehicle", "vehicle"))
        self.assertEqual(normalise("Hot Air Balloon"), ("hot_air_balloon", "other"))

    def test_parse_names(self):
        self.assertEqual(parse_names({"names": "{0: 'person', 1: 'car'}"}), ["person", "car"])
        self.assertEqual(parse_names({"names": "person,car,truck"}), ["person", "car", "truck"])
        self.assertEqual(parse_names({}), [])


class TestNMS(unittest.TestCase):
    def test_suppresses_overlaps_keeps_separate(self):
        boxes = np.array([[0, 0, 10, 10], [1, 1, 11, 11], [50, 50, 60, 60]], dtype=np.float32)
        scores = np.array([0.9, 0.8, 0.7], dtype=np.float32)
        self.assertEqual(nms(boxes, scores, 0.5), [0, 2])

    def test_smaller_box_overlap_removes_clipped_duplicate(self):
        whole = [0, 0, 20, 10]
        clipped = [10, 0, 20, 10]  # the right half, as a tile at x=10 would see it
        boxes = np.array([whole, clipped], dtype=np.float32)
        scores = np.array([0.9, 0.8], dtype=np.float32)
        self.assertEqual(nms(boxes, scores, 0.6), [0, 1], "IoU 0.5 keeps both")
        self.assertEqual(nms(boxes, scores, 0.6, smaller=True), [0])


class TestTracker(unittest.TestCase):
    def test_confirms_after_min_hits_once(self):
        tr = Tracker(min_hits=3)
        confirmed_total = []
        for i in range(5):
            pairs, confirmed = tr.update([det(0.5 + i * 0.002, 0.5)], now=i * 0.33)
            confirmed_total += confirmed
            self.assertEqual(pairs[0][0].id, 1, "same object keeps its id")
        self.assertEqual([t.id for t in confirmed_total], [1], "reported exactly once, on the third frame")

    def test_fast_small_object_matched_by_distance(self):
        tr = Tracker()
        tr.update([det(0.50, 0.5)], 0.0)
        # Moved 1.5 box widths: no overlap, but close.
        pairs, _ = tr.update([det(0.53, 0.5)], 0.33)
        self.assertEqual(pairs[0][0].id, 1)
        self.assertEqual(iou((0.50, 0.5, 0.02, 0.04), (0.53, 0.5, 0.02, 0.04)), 0.0)
        self.assertLess(centre_distance((0.50, 0.5, 0.02, 0.04), (0.53, 0.5, 0.02, 0.04)), 2.0)

    def test_groups_do_not_mix_and_old_tracks_expire(self):
        tr = Tracker(max_age_s=1.0)
        tr.update([det(0.5, 0.5)], 0.0)
        pairs, _ = tr.update([det(0.5, 0.5, label="car", group="vehicle")], 0.3)
        self.assertEqual(pairs[0][0].id, 2, "a car never continues a person track")
        tr.update([], 2.0)
        self.assertEqual(tr.tracks, [])


@unittest.skipUnless(os.environ.get("MODEL_VISIBLE") and os.path.exists(os.environ["MODEL_VISIBLE"]), "no model")
class TestModelRuns(unittest.TestCase):
    def test_runs_on_grey_and_odd_sizes(self):
        from detector.model import YoloOnnx
        m = YoloOnnx(os.environ["MODEL_VISIBLE"])
        for shape in [(720, 1280, 3), (288, 384), (512, 640, 3)]:
            dets, ms = m.detect(np.full(shape, 90, dtype=np.uint8))
            self.assertIsInstance(dets, list)
            self.assertGreater(ms, 0)
            for d in dets:
                x, y, w, h = d.box
                self.assertTrue(0 <= x <= 1 and 0 <= y <= 1 and w > 0 and h > 0)


if __name__ == "__main__":
    unittest.main()
