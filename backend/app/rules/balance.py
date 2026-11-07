"""Проверка остатка на счёте и остатка по договору."""
from __future__ import annotations

from .. import refdata
from ..models import BalanceCheck, BalanceOption
from .common import fmt_money
from .money import convert, rate, spread_pct


def account_by_id(account_id: str | None) -> dict | None:
    for a in refdata.accounts():
        if a['id'] == account_id or (account_id and a['iban'] == account_id.replace(' ', '')):
            return a
    return None


def account_for_currency(cur: str | None) -> dict | None:
    for a in refdata.accounts():
        if a['currency'] == cur:
            return a
    return None


def kzt_for(amount: float, currency: str) -> float:
    """Сколько тенге спишут при конвертации с учётом спреда."""
    return round(amount * rate(currency) * (1 + spread_pct() / 100), 2)


def check_balance(account_id: str | None, amount: float, sender_fees: float, currency: str) -> BalanceCheck:
    acc = account_by_id(account_id)
    need_cur = round(float(amount or 0) + float(sender_fees or 0), 2)
    if acc is None:
        available, needed, acur = 0.0, need_cur, currency
    elif acc['currency'] == currency:
        available, needed, acur = float(acc['balance']), need_cur, currency
    else:
        acur = acc['currency']
        available = float(acc['balance'])
        needed = kzt_for(need_cur, currency) if acur == 'KZT' else convert(need_cur * (1 + spread_pct() / 100),
                                                                            currency, acur)
    ok = available >= needed
    shortfall = round(max(0.0, needed - available), 2)
    options: list[BalanceOption] = []
    if not ok:
        kzt = account_for_currency('KZT')
        short_cur = shortfall if acur == currency else round(shortfall * rate(acur) / rate(currency), 2)
        if kzt and kzt['id'] != (acc or {}).get('id'):
            kzt_need = kzt_for(short_cur, currency)
            enough = kzt['balance'] >= kzt_need
            options.append(BalanceOption(
                kind='convert', from_account_id=kzt['id'], amount=kzt_need,
                title=f'Купить {fmt_money(short_cur, currency)} за тенге',
                detail=(f'С тенгового счёта спишут примерно {fmt_money(kzt_need, "KZT")} по курсу '
                        f'{fmt_money(rate(currency), "KZT")} за 1 {currency} плюс {str(spread_pct()).replace(".", ",")}% '
                        f'за конвертацию. На тенговом счёте {fmt_money(kzt["balance"], "KZT")}'
                        + ('.' if enough else ', этого тоже не хватает.'))))
        if acc is not None and available > 0 and acc['currency'] == currency:
            part = round(max(0.0, available - float(sender_fees or 0)), 2)
            if part > 0:
                options.append(BalanceOption(
                    kind='partial', from_account_id=acc['id'], amount=part,
                    title=f'Оплатить часть: {fmt_money(part, currency)}',
                    detail=('Заплатить сейчас столько, сколько есть на счёте за вычетом комиссии, остаток доплатить '
                            'позже. Частичную оплату лучше заранее согласовать с поставщиком.')))
    return BalanceCheck(ok=ok, currency=acur, available=round(available, 2), needed=needed, shortfall=shortfall,
                        options=options)


def contract_balance(total: float | None, payments, currency: str | None = None) -> tuple[float, float | None]:
    paid = 0.0
    for p in payments or []:
        amt = p.amount if hasattr(p, 'amount') else p.get('amount', 0)
        cur = p.currency if hasattr(p, 'currency') else p.get('currency')
        if currency and cur and cur != currency:
            amt = convert(amt, cur, currency)
        paid += float(amt)
    paid = round(paid, 2)
    return paid, (round(float(total) - paid, 2) if total is not None else None)
