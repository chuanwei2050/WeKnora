#!/usr/bin/env python3
import argparse
from uuid import UUID

from structured_query.reindex import rebuild_profile_index


def main() -> None:
    parser = argparse.ArgumentParser(description="Rebuild the Milvus profile index from active PostgreSQL metadata")
    parser.add_argument("--version", type=UUID, help="only rebuild one active dataset version")
    args = parser.parse_args()
    result = rebuild_profile_index(args.version)
    print(f"rebuilt versions={result['versions']} documents={result['documents']}")


if __name__ == "__main__":
    main()
