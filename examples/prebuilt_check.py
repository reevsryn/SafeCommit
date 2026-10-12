"""TEMPORARY: gives the prebuilt binary something real to find."""

import os  # stdlib, must be suppressed
import profiling  # Python 3.15 stdlib, must be suppressed (the v1.0.1 fix)

import safecommit_demo_not_a_real_package  # deliberately absent from PyPI


def main() -> None:
    print(os.getcwd(), profiling, safecommit_demo_not_a_real_package.VERSION)
