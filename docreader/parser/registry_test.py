from docreader.parser.registry import registry


def test_builtin_pptx_falls_back_to_markitdown():
    cls = registry.get_parser_class("builtin", "pptx")
    assert cls.__name__ == "MarkitdownParser"


def test_markitdown_pptx_uses_markitdown():
    cls = registry.get_parser_class("markitdown", "pptx")
    assert cls.__name__ == "MarkitdownParser"


def test_builtin_docx_stays_builtin():
    cls = registry.get_parser_class("builtin", "docx")
    assert cls.__name__ == "Docx2Parser"
