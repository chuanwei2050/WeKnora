from pathlib import Path
import sys
import os

from playwright.sync_api import sync_playwright


OUTPUT = Path(__file__).with_name("structured-query-browser.png")
sys.stdout.reconfigure(encoding="utf-8")


with sync_playwright() as playwright:
    browser = playwright.chromium.launch(headless=True)
    page = browser.new_page(viewport={"width": 1440, "height": 1000})
    console_errors: list[str] = []
    page.on("console", lambda message: console_errors.append(message.text) if message.type == "error" else None)
    response = page.goto(os.getenv("WEKNORA_BROWSER_URL", "http://127.0.0.1:5174"), wait_until="networkidle")
    if page.url.endswith("/login"):
        page.get_by_placeholder("请输入用户名").fill(os.getenv("WEKNORA_BROWSER_USER", "admin"))
        page.get_by_placeholder("请输入登录密码").fill(os.environ["WEKNORA_BROWSER_PASSWORD"])
        page.get_by_role("button", name="进入平台").click()
        page.wait_for_timeout(2000)
    page.screenshot(path=str(OUTPUT), full_page=True)
    print({
        "status": response.status if response else None,
        "url": page.url,
        "title": page.title(),
        "text": page.locator("body").inner_text()[:4000],
        "console_errors": console_errors,
        "screenshot": str(OUTPUT),
    })
    browser.close()
