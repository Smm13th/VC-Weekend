"""Поиск скрытых инструкций для ИИ в документах (prompt injection)."""
from __future__ import annotations

import re

from ..models import ParsedDoc, SourceRef
from .common import line_spans, ref

PATTERNS = [re.compile(p, re.I) for p in [
    r'\b(ignore|disregard|forget|override|bypass)\b[^\n]{0,40}\b(previous|prior|above|earlier|all|any|your)\b[^\n]{0,30}\b(instructions?|prompts?|rules|messages?|guidelines)',
    r'\bnote\s+(to|for)\s+(the\s+)?(ai|assistant|bot|llm|model|chatbot|gpt)\b',
    r'\b(ai|llm|virtual)\s+assistant\s*[:,]',
    r'\bsystem\s+prompt\b',
    r'\byou\s+are\s+(now\s+)?(an?\s+)?(ai|assistant|chatbot|language\s+model|llm)\b',
    r'\b(as\s+an?\s+ai|dear\s+(ai|assistant|model))\b',
    r'\b(confirm|mark|treat)\b[^\n]{0,60}\bas\s+(the\s+)?(verified|trusted|approved|confirmed)\b',
    r'игнорир\w*[^\n]{0,40}(инструкц|указани|правил)',
    r'(не\s+обращай\w*\s+внимани\w*|забудь\w*)[^\n]{0,40}(инструкц|указани|правил)',
    r'(примечание|заметка|сообщение|указание|инструкция)\s+для\s+(ии|ai|ассистента|помощника|бота|нейросети|модели)',
    r'\bты\s*[—–-]?\s*(ии|ассистент|помощник|бот|нейросеть|языковая\s+модель)\b',
    r'системн\w+\s+(промпт|подсказк|инструкц)',
    r'подтверди\w*[^\n]{0,60}как\s+(проверенн|надёжн|надежн|подтверждённ)',
]]


def detect_injections(doc: ParsedDoc) -> list[SourceRef]:
    out: list[SourceRef] = []
    seen = set()
    for s, e, _b in line_spans(doc):
        line = doc.text[s:e]
        if any(p.search(line) for p in PATTERNS):
            r = ref(doc, s, e)
            if (r.start, r.end) not in seen and r.quote:
                seen.add((r.start, r.end))
                out.append(r)
    return out
