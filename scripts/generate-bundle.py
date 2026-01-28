#!/usr/bin/env python3
"""
Generate OLM bundle for ST4SD operator.

This script replicates the functionality of generate-bundle.sh in Python,
with improved argument handling and automatic default value extraction.
"""

import argparse
import os
import shutil
import subprocess
import sys
from pathlib import Path
from typing import Optional

try:
    import yaml
except ImportError:
    print(
        "Error: PyYAML is required. Install with: pip install pyyaml", file=sys.stderr
    )
    sys.exit(1)


def get_default_bundle_version() -> str:
    """
    Extract appVersion from st4sd-deployment/helm-chart/Chart.yaml.

    Returns:
        The appVersion value from Chart.yaml

    Raises:
        FileNotFoundError: If Chart.yaml doesn't exist
        KeyError: If appVersion field is missing
    """
    chart_path = Path("st4sd-deployment/helm-chart/Chart.yaml")

    if not chart_path.exists():
        raise FileNotFoundError(f"Chart.yaml not found at {chart_path}")

    with open(chart_path) as f:
        chart_data = yaml.safe_load(f)

    if "appVersion" not in chart_data:
        raise KeyError("appVersion field not found in Chart.yaml")

    return chart_data["appVersion"]


def get_default_olm_version() -> str:
    """
    Extract VERSION from scripts/constants.sh by sourcing the file and reading $VERSION.

    Returns:
        The VERSION value from constants.sh

    Raises:
        FileNotFoundError: If constants.sh doesn't exist
        ValueError: If VERSION cannot be extracted
        subprocess.CalledProcessError: If bash command fails
    """
    constants_path = Path("scripts/constants.sh")

    if not constants_path.exists():
        raise FileNotFoundError(f"constants.sh not found at {constants_path}")

    # Source the constants.sh file and echo the VERSION variable
    bash_command = f'. {constants_path} && echo "$VERSION"'

    result = subprocess.run(
        ["bash", "-c", bash_command], capture_output=True, text=True, check=True
    )

    version = result.stdout.strip()

    if not version:
        raise ValueError("VERSION variable is empty or not set in constants.sh")

    return version


def parse_arguments():
    """
    Parse command-line arguments.

    Returns:
        Namespace object with parsed arguments
    """
    parser = argparse.ArgumentParser(
        description="Generate OLM bundle for ST4SD operator",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  # Use all defaults
  %(prog)s
  
  # Custom bundle version only
  %(prog)s --bundle-version 2.7.0
  
  # Custom OLM version only
  %(prog)s --olm-version 0.11.0
  
  # All custom values
  %(prog)s --bundle-version 2.7.0 --olm-version 0.11.0 --image-prefix quay.io/myorg/st4sd-olm
        """,
    )

    parser.add_argument(
        "--bundle-version", help="Bundle version (default: appVersion from Chart.yaml)"
    )
    parser.add_argument(
        "--olm-version", help="OLM version (default: VERSION from constants.sh)"
    )
    parser.add_argument(
        "--image-prefix",
        default="quay.io/st4sd/official-base/st4sd-olm",
        help="Container image prefix (default: quay.io/st4sd/official-base/st4sd-olm)",
    )

    return parser.parse_args()


def create_bundle_structure(bundle_dir: Path) -> None:
    """
    Remove old bundle directory and create new structure.

    Args:
        bundle_dir: Path to bundle directory
    """
    # Remove old bundle if it exists
    if bundle_dir.exists():
        print(f"Removing old bundle directory: {bundle_dir}")
        shutil.rmtree(bundle_dir)

    # Create directory structure
    manifests_dir = bundle_dir / "manifests"
    metadata_dir = bundle_dir / "metadata"

    print(f"Creating bundle structure:")
    print(f"  {manifests_dir}")
    print(f"  {metadata_dir}")

    manifests_dir.mkdir(parents=True)
    metadata_dir.mkdir(parents=True)


def generate_annotations_yaml(bundle_dir: Path) -> None:
    """
    Generate bundle/metadata/annotations.yaml with OLM metadata.

    Args:
        bundle_dir: Path to bundle directory
    """
    annotations_path = bundle_dir / "metadata" / "annotations.yaml"

    annotations_content = """annotations:
  # Core bundle annotations.
  operators.operatorframework.io.bundle.mediatype.v1: registry+v1
  operators.operatorframework.io.bundle.manifests.v1: manifests/
  operators.operatorframework.io.bundle.metadata.v1: metadata/
  operators.operatorframework.io.bundle.package.v1: st4sd-olm
  operators.operatorframework.io.bundle.channels.v1: alpha
  operators.operatorframework.io.metrics.builder: operator-sdk-v1.26.0
  operators.operatorframework.io.metrics.mediatype.v1: metrics+v1
  operators.operatorframework.io.metrics.project_layout: go.kubebuilder.io/v3
"""

    print(f"Generating {annotations_path}")
    with open(annotations_path, "w") as f:
        f.write(annotations_content)


def run_make_manifests() -> None:
    """
    Execute 'make manifests' to ensure CRD is up-to-date.

    Raises:
        subprocess.CalledProcessError: If make command fails
    """
    print("Running 'make manifests' to ensure CRD is up-to-date...")
    result = subprocess.run(["make", "manifests"], capture_output=True, text=True)

    if result.returncode != 0:
        print(f"Error running 'make manifests':", file=sys.stderr)
        print(result.stderr, file=sys.stderr)
        raise subprocess.CalledProcessError(result.returncode, "make manifests")

    print("✓ CRD manifests updated")


def copy_crd_file(bundle_dir: Path) -> None:
    """
    Copy CRD file to bundle/manifests/.

    Args:
        bundle_dir: Path to bundle directory

    Raises:
        FileNotFoundError: If CRD file doesn't exist
    """
    crd_source = Path("config/crd/bases/deploy.st4sd.ibm.com_simulationtoolkits.yaml")
    crd_dest = bundle_dir / "manifests" / "deploy.st4sd.ibm.com_simulationtoolkits.yaml"

    if not crd_source.exists():
        raise FileNotFoundError(f"CRD file not found at {crd_source}")

    print(f"Copying CRD: {crd_source} -> {crd_dest}")
    shutil.copy2(crd_source, crd_dest)


def process_csv_template(bundle_dir: Path, img_operator: str, version: str) -> None:
    """
    Read CSV template, replace placeholders, and write to bundle.

    Args:
        bundle_dir: Path to bundle directory
        img_operator: Full container image URL (e.g., quay.io/st4sd/official-base/st4sd-olm:v2.6.0)
        version: OLM version string

    Raises:
        FileNotFoundError: If CSV template doesn't exist
    """
    csv_template = Path("config/manifests/st4sd-olm.clusterserviceversion.yaml")
    csv_dest = bundle_dir / "manifests" / "st4sd-olm.clusterserviceversion.yaml"

    if not csv_template.exists():
        raise FileNotFoundError(f"CSV template not found at {csv_template}")

    print(f"Processing CSV template: {csv_template}")
    print(
        f"  Replacing 'quay.io/st4sd/official-base/st4sd-olm%%' with '{img_operator}'"
    )
    print(f"  Replacing '%%VERSION%%' with '{version}'")

    with open(csv_template) as f:
        content = f.read()

    # Replace placeholders
    content = content.replace("quay.io/st4sd/official-base/st4sd-olm", img_operator)
    content = content.replace("%%VERSION%%", version)

    print(f"Writing processed CSV to: {csv_dest}")
    with open(csv_dest, "w") as f:
        f.write(content)


def run_make_bundle_build(version: str) -> None:
    """
    Execute 'make bundle-build' with VERSION environment variable.

    Args:
        version: OLM version string

    Raises:
        subprocess.CalledProcessError: If make command fails
    """
    print(f"Running 'make bundle-build' with VERSION={version}...")

    env = os.environ.copy()
    env["VERSION"] = version

    result = subprocess.run(
        ["make", "bundle-build"], env=env, capture_output=True, text=True
    )

    if result.returncode != 0:
        print(f"Error running 'make bundle-build':", file=sys.stderr)
        print(result.stderr, file=sys.stderr)
        raise subprocess.CalledProcessError(result.returncode, "make bundle-build")

    print(f"✓ Bundle image built: st4sd-olm-bundle:v{version}")


def copy_to_bundles(bundle_dir: Path, version: str) -> None:
    """
    Copy bundle to bundles/v{version}/ directory.

    Args:
        bundle_dir: Path to bundle directory
        version: OLM version string
    """
    bundles_dir = Path("bundles")
    version_dir = bundles_dir / f"v{version}"

    # Remove old versioned bundle if it exists
    if version_dir.exists():
        print(f"Removing old versioned bundle: {version_dir}")
        shutil.rmtree(version_dir)

    print(f"Copying bundle to: {version_dir}")
    shutil.copytree(bundle_dir, version_dir)
    print(f"✓ Bundle copied to {version_dir}")


def main():
    """Main execution flow."""
    try:
        # Parse arguments
        args = parse_arguments()

        # Get or extract default values
        print("=" * 60)
        print("ST4SD OLM Bundle Generator")
        print("=" * 60)

        if args.bundle_version:
            bundle_version = args.bundle_version
            print(f"Bundle version: {bundle_version} (from command line)")
        else:
            bundle_version = get_default_bundle_version()
            print(f"Bundle version: {bundle_version} (from Chart.yaml)")

        if args.olm_version:
            olm_version = args.olm_version
            print(f"OLM version: {olm_version} (from command line)")
        else:
            olm_version = get_default_olm_version()
            print(f"OLM version: {olm_version} (from constants.sh)")

        image_prefix = args.image_prefix
        print(f"Image prefix: {image_prefix}")

        # Construct full image operator URL
        img_operator = f"{image_prefix}:v{bundle_version}"
        print(f"Image operator: {img_operator}")
        print("=" * 60)
        print()

        # Change to project root (parent of scripts directory)
        script_dir = Path(__file__).parent
        project_root = script_dir.parent
        os.chdir(project_root)
        print(f"Working directory: {project_root}")
        print()

        # Execute bundle generation steps
        bundle_dir = Path("bundle")

        create_bundle_structure(bundle_dir)
        print()

        generate_annotations_yaml(bundle_dir)
        print()

        run_make_manifests()
        print()

        copy_crd_file(bundle_dir)
        print()

        process_csv_template(bundle_dir, img_operator, olm_version)
        print()

        run_make_bundle_build(olm_version)
        print()

        copy_to_bundles(bundle_dir, olm_version)
        print()

        print("=" * 60)
        print("✓ Bundle generation completed successfully!")
        print(f"  Bundle directory: {bundle_dir}")
        print(f"  Versioned bundle: bundles/v{olm_version}")
        print(f"  Bundle image: st4sd-olm-bundle:v{olm_version}")
        print("=" * 60)

    except KeyboardInterrupt:
        print("\n\nOperation cancelled by user", file=sys.stderr)
        sys.exit(130)
    except Exception as e:
        print(f"\n\nError: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()

# Made with Bob
