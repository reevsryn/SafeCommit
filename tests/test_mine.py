"""Unit tests for the mining gates and diff scanning (no network)."""
import unittest

from bench.diffscan import diff_stats, iter_added_lines, post_image_paths
from bench.mine import _is_requirement_like, diff_adds_import_or_dep, diff_touches_python

DIFF = """\
diff --git a/pkg/mod.py b/pkg/mod.py
--- a/pkg/mod.py
+++ b/pkg/mod.py
@@ -1,2 +1,4 @@
+import os
+from foo import bar
 x = 1
-y = 2
+y = 3
diff --git a/README.md b/README.md
--- a/README.md
+++ b/README.md
@@ -1 +1,2 @@
+import nothing  (this line is markdown prose)
diff --git a/gone.py b/gone.py
--- a/gone.py
+++ /dev/null
@@ -1 +0,0 @@
-import dead
"""

MD_ONLY = """\
diff --git a/README.md b/README.md
--- a/README.md
+++ b/README.md
@@ -1 +1,2 @@
+import looks_like_code
"""

REQS_ONLY = """\
diff --git a/requirements.txt b/requirements.txt
--- a/requirements.txt
+++ b/requirements.txt
@@ -1 +1,3 @@
+# a comment
+requests>=2.0
"""

REQS_COMMENT_ONLY = """\
diff --git a/requirements.txt b/requirements.txt
--- a/requirements.txt
+++ b/requirements.txt
@@ -1 +1,2 @@
+# only a comment
"""


class TestDiffScan(unittest.TestCase):
    def test_iter_added_lines(self):
        added = list(iter_added_lines(DIFF))
        self.assertIn(("pkg/mod.py", "import os"), added)
        self.assertIn(("pkg/mod.py", "from foo import bar"), added)
        self.assertIn(("pkg/mod.py", "y = 3"), added)
        # deleted file contributes no added lines; its -line is not yielded
        self.assertTrue(all(path != "gone.py" for path, _ in added))

    def test_post_image_paths_excludes_deleted(self):
        self.assertEqual(post_image_paths(DIFF), ["pkg/mod.py", "README.md"])

    def test_diff_stats(self):
        files, changed = diff_stats(DIFF)
        self.assertEqual(files, 3)
        # +import os, +from..., -y=2, +y=3, +markdown line, -import dead
        self.assertEqual(changed, 6)


class TestGates(unittest.TestCase):
    def test_touches_python(self):
        self.assertTrue(diff_touches_python(DIFF))
        self.assertFalse(diff_touches_python(MD_ONLY))

    def test_adds_import_in_py(self):
        self.assertTrue(diff_adds_import_or_dep(DIFF))

    def test_markdown_import_does_not_count(self):
        self.assertFalse(diff_adds_import_or_dep(MD_ONLY))

    def test_requirements_line_counts(self):
        self.assertTrue(diff_adds_import_or_dep(REQS_ONLY))

    def test_requirements_comment_does_not_count(self):
        self.assertFalse(diff_adds_import_or_dep(REQS_COMMENT_ONLY))


class TestRequirementLike(unittest.TestCase):
    def test_requirements_txt(self):
        self.assertTrue(_is_requirement_like("requirements.txt", "requests>=2.0"))
        self.assertTrue(_is_requirement_like("requirements.txt", "numpy"))
        self.assertFalse(_is_requirement_like("requirements.txt", "# pinned"))
        self.assertFalse(_is_requirement_like("requirements.txt", "-r base.txt"))
        self.assertFalse(
            _is_requirement_like("requirements.txt", "git+https://github.com/x/y.git")
        )

    def test_pyproject_specs(self):
        self.assertTrue(_is_requirement_like("pyproject.toml", '"httpx>=0.27",'))
        self.assertTrue(_is_requirement_like("pyproject.toml", '"numpy",'))
        self.assertFalse(_is_requirement_like("pyproject.toml", "dependencies = ["))
        self.assertFalse(_is_requirement_like("pyproject.toml", "[project]"))

    def test_setup_py_kwargs_do_not_count(self):
        self.assertFalse(_is_requirement_like("setup.py", 'name="safecommit",'))
        self.assertFalse(_is_requirement_like("setup.py", 'python_requires=">=3.9",'))


if __name__ == "__main__":
    unittest.main()
