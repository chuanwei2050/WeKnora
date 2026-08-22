#!/usr/bin/env python3
"""
WeKnora MCP Server 模组测试脚本

测试模组的各种启动方式和功能
"""

import os
import subprocess
import sys
from pathlib import Path


def test_imports():
    """测试模块导入"""
    print("=== 测试模块导入 ===")

    import mcp
    import requests
    import weknora_mcp_server
    from weknora_mcp_server import WeKnoraClient, run
    import main

    assert mcp is not None
    assert requests is not None
    assert weknora_mcp_server is not None
    assert WeKnoraClient is not None
    assert run is not None
    assert main is not None


def test_environment():
    """测试环境配置"""
    print("\n=== 测试环境配置 ===")

    base_url = os.getenv("WEKNORA_BASE_URL")
    api_key = os.getenv("WEKNORA_API_KEY")

    print(f"WEKNORA_BASE_URL: {base_url or '未设置 (将使用默认值)'}")
    print(f"WEKNORA_API_KEY: {'已设置' if api_key else '未设置'}")

    if not base_url:
        print("提示: 可以设置环境变量 WEKNORA_BASE_URL")

    if not api_key:
        print("提示: 建议设置环境变量 WEKNORA_API_KEY")



def test_client_creation():
    """测试客户端创建"""
    print("\n=== 测试客户端创建 ===")

    from weknora_mcp_server import WeKnoraClient

    base_url = os.getenv("WEKNORA_BASE_URL", "http://localhost:8080/api/v1")
    api_key = os.getenv("WEKNORA_API_KEY", "test_key")
    client = WeKnoraClient(base_url, api_key)
    assert client.base_url == base_url
    assert client.api_key == api_key


def test_file_structure():
    """测试文件结构"""
    print("\n=== 测试文件结构 ===")

    required_files = [
        "__init__.py",
        "main.py",
        "run_server.py",
        "weknora_mcp_server.py",
        "requirements.txt",
        "setup.py",
        "pyproject.toml",
        "README.md",
        "INSTALL.md",
        "LICENSE",
        "MANIFEST.in",
    ]

    missing_files = []
    for file in required_files:
        if Path(file).exists():
            print(f"✓ {file}")
        else:
            print(f"✗ {file} (缺失)")
            missing_files.append(file)

    assert not missing_files, f"缺失文件: {missing_files}"


def test_entry_points():
    """测试入口点"""
    print("\n=== 测试入口点 ===")

    result = subprocess.run(
        [sys.executable, "main.py", "--help"],
        capture_output=True,
        text=True,
        timeout=10,
    )
    assert result.returncode == 0, result.stderr

    result = subprocess.run(
        [sys.executable, "main.py", "--check-only"],
        capture_output=True,
        text=True,
        timeout=10,
    )
    assert result.returncode == 0, result.stderr


def test_package_installation():
    """测试包安装 (开发模式)"""
    print("\n=== 测试包安装 ===")

    result = subprocess.run(
        [sys.executable, "setup.py", "check"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, result.stderr


def main():
    """运行所有测试"""
    print("WeKnora MCP Server 模组测试")
    print("=" * 50)

    tests = [
        ("模块导入", test_imports),
        ("环境配置", test_environment),
        ("客户端创建", test_client_creation),
        ("文件结构", test_file_structure),
        ("入口点", test_entry_points),
        ("包安装", test_package_installation),
    ]

    passed = 0
    total = len(tests)

    for test_name, test_func in tests:
        try:
            test_func()
            passed += 1
        except Exception as e:
            print(f"测试异常: {test_name} - {e}")

    print("\n" + "=" * 50)
    print(f"测试结果: {passed}/{total} 通过")

    if passed == total:
        print("✓ 所有测试通过！模组可以正常使用。")
        return True
    else:
        print("✗ 部分测试失败，请检查上述错误。")
        return False


if __name__ == "__main__":
    success = main()
    sys.exit(0 if success else 1)
