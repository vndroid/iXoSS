"""Negative checks for source provenance and review-ledger integrity."""
import copy
import json
from pathlib import Path
import subprocess
import shutil
import sys
import tempfile
import unittest

import audit_imgproxy as audit
import review_imgproxy as review


class AuditValidation(unittest.TestCase):
    def setUp(self):
        self.lock = json.loads(audit.LOCK.read_text())
        self.reviews = json.loads(audit.REVIEWS.read_text())

    def test_current_records(self):
        audit.validate(self.lock, self.reviews)

    def test_corrupt_records_are_rejected(self):
        mutations = [
            lambda l, r: l['files'].append(copy.deepcopy(l['files'][0])),
            lambda l, r: l['baseline'].update(commit='bad'),
            lambda l, r: l['files'][0].update(upstream_path='../outside'),
            lambda l, r: l['files'][0].update(comparison='unknown'),
            lambda l, r: l['summary'].update(modified=999),
            lambda l, r: l['review_progress'].update(complete=False),
            lambda l, r: r['intervals'][0]['commits'][0].update(status='pending_analysis'),
            lambda l, r: r['intervals'][0]['commits'][0].update(reason=''),
            lambda l, r: r['intervals'][0]['commits'][2].update(evidence=[]),
            lambda l, r: l.update(port_diff_sha256='0' * 64),
            lambda l, r: l['backported_security_commits'][0].update(commit='0' * 40),
        ]
        for index, mutation in enumerate(mutations):
            with self.subTest(index=index):
                lock, reviews = copy.deepcopy(self.lock), copy.deepcopy(self.reviews)
                mutation(lock, reviews)
                with self.assertRaises(ValueError):
                    audit.validate(lock, reviews)

    def test_source_added_changed_or_removed(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for entry in self.lock['files']:
                source = audit.ROOT / entry['path']
                target = root / entry['path']
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(source, target)
            for path in [audit.LOCK, audit.REVIEWS, audit.DIFF, audit.ROOT / 'tools/audit_imgproxy.py',
                         audit.ROOT / 'third_party/imgproxy/NOTICE.upstream']:
                target = root / path.relative_to(audit.ROOT)
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(path, target)
            def run():
                return subprocess.run([sys.executable, str(root / 'tools/audit_imgproxy.py')],
                                      capture_output=True, text=True)
            self.assertEqual(run().returncode, 0)
            file = root / self.lock['files'][0]['path']
            data = file.read_bytes()
            file.write_bytes(data + b'// unrecorded change\n')
            self.assertNotEqual(run().returncode, 0)
            file.write_bytes(data)
            file.unlink()
            self.assertNotEqual(run().returncode, 0)
            file.write_bytes(data)
            extra = file.with_name('unrecorded.go')
            extra.write_bytes(audit.NOTICE_HEADER + b'package fixture\n')
            self.assertNotEqual(run().returncode, 0)

    def test_comparison_includes_unmapped_commits(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            def git(*args):
                return subprocess.check_output(['git', '-C', directory, *args], text=True).strip()
            git('init', '-q')
            git('config', 'user.name', 'Fixture')
            git('config', 'user.email', 'fixture@example.invalid')
            git('config', 'commit.gpgsign', 'false')
            (root / 'initial').write_text('initial')
            git('add', 'initial'); git('commit', '-qm', 'initial')
            start = git('rev-parse', 'HEAD')
            (root / 'unmapped').write_text('review this too')
            git('add', 'unmapped'); git('commit', '-qm', 'ordinary change')
            data = review.report(root, start, 'HEAD', self.lock)
            self.assertEqual(len(data['commits']), 1)
            self.assertEqual(data['commits'][0]['upstream_paths'], ['unmapped'])
            self.assertEqual(data['commits'][0]['status'], 'pending_analysis')
            with self.assertRaises(subprocess.CalledProcessError):
                review.report(root, 'HEAD', start, self.lock)


if __name__ == '__main__':
    unittest.main()
