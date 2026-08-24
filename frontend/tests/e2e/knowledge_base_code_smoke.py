from pathlib import Path
import json
import subprocess
import re

from playwright.sync_api import sync_playwright


output = Path(__file__).resolve().parent / "knowledge-base-code-smoke.png"
public_folder_output = Path(__file__).resolve().parent / "public-file-folder-smoke.png"

with sync_playwright() as playwright:
    browser = playwright.chromium.launch(headless=True)
    page = browser.new_page(viewport={"width": 1440, "height": 1000})
    page.goto("http://127.0.0.1:5173", wait_until="networkidle")
    defaults = (Path(__file__).resolve().parents[3] / "internal/application/service/user.go").read_text(encoding="utf-8")

    def credential(env_name: str, constant_name: str) -> str:
        result = subprocess.run(
            ["docker", "exec", "WeKnora-app-dev", "printenv", env_name],
            text=True, capture_output=True, check=False,
        )
        if result.returncode == 0 and result.stdout.strip():
            return result.stdout.strip()
        match = re.search(rf'{constant_name}\s*=\s*"([^"]+)"', defaults)
        assert match is not None
        return match.group(1)

    username = credential("DEFAULT_ADMIN_USERNAME", "defaultAdminUsername")
    password = credential("DEFAULT_ADMIN_PASSWORD", "defaultAdminPassword")
    page.get_by_placeholder("请输入用户名").fill(username)
    page.get_by_placeholder("请输入登录密码").fill(password)
    page.get_by_role("button", name="进入平台").click()
    page.wait_for_url(lambda url: "/login" not in url, timeout=15_000)
    page.locator(".tenant-trigger").click()
    page.locator(".tenant-item:not(.platform-item)").first.click()
    page.wait_for_url("**/platform/knowledge-bases", timeout=15_000)
    page.wait_for_load_state("networkidle")
    page.locator(".header-action-btn").click(timeout=10_000)
    page.get_by_text("知识库编码", exact=True).wait_for()

    labels = page.locator(".form-label").all_inner_texts()
    name_index = labels.index("知识库名称")
    code_index = labels.index("知识库编码")
    description_index = labels.index("知识库描述")
    assert code_index == name_index + 1
    assert description_index == code_index + 1
    code_input = page.get_by_placeholder("用于筛选知识库（可选）")
    code_input.fill("A" * 65)
    assert len(code_input.input_value()) == 64
    code_input.fill("RAG_KB-2026")
    assert code_input.input_value() == "RAG_KB-2026"

    page.locator(".close-btn").click()
    page.locator(".settings-overlay").wait_for(state="hidden")
    page.locator(".kb-card.kb-type-document:not(.shared-kb-card)").first.click()
    page.wait_for_url("**/platform/knowledge-bases/*", timeout=15_000)
    public_folder = page.locator(".public-folder-row")
    public_folder.wait_for()
    assert public_folder.locator(".tree-name").inner_text() == "公共文件"
    assert public_folder.locator(".tree-count").count() == 0
    page.screenshot(path=str(public_folder_output), full_page=True)

    api_response = page.request.get("http://127.0.0.1:8080/api/integration/v1/knowledge-bases/by-code/invalid%2Fcode/folders")
    assert api_response.status == 401
    print(json.dumps({"url": page.url, "code_field": "ok", "unauthenticated_api_status": api_response.status}, ensure_ascii=True))
    page.screenshot(path=str(output), full_page=True)
    browser.close()
