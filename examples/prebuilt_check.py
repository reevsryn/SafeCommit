"""TEMPORARY: gives the prebuilt binary something real to find."""

import os  # stdlib, must be suppressed

import safecommit_demo_not_a_real_package  # deliberately absent from PyPI


def main() -> None:
    print(os.getcwd(), safecommit_demo_not_a_real_package.VERSION)
