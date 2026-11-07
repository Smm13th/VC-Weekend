"""Симулятор комиссий OUR / SHA / BEN по tariffs.json."""
from __future__ import annotations

from .. import refdata
from ..models import FeeLine, FeeSim
from .common import fmt_num
from .money import convert


def _tariff(table: dict, cur: str) -> float:
    if cur in table:
        return float(table[cur])
    return convert(float(table['USD']), 'USD', cur)


def sender_fee(amount: float, currency: str) -> float:
    t = refdata.tariffs()['senderFee']
    fee = amount * t['pct'] / 100
    lo, hi = _tariff(t['min'], currency), _tariff(t['max'], currency)
    return round(min(max(fee, lo), hi), 2)


def simulate_fees(amount: float, currency: str, fee_type: str, urgent: bool = False) -> FeeSim:
    t = refdata.tariffs()
    amount = round(float(amount or 0), 2)
    fee_type = (fee_type or 'OUR').upper()
    sf = sender_fee(amount, currency) if amount > 0 else 0.0
    pct = t['senderFee']['pct']
    lo, hi = _tariff(t['senderFee']['min'], currency), _tariff(t['senderFee']['max'], currency)
    sf_label = (f'Комиссия нашего банка за перевод: {fmt_num(pct, 1)}% от суммы, не меньше {fmt_num(lo, 0)} '
                f'и не больше {fmt_num(hi, 0)} {currency}')
    inter = _tariff(t['intermediaryFee'], currency)
    benf = _tariff(t['beneficiaryBankFee'], currency)
    guar = _tariff(t['ourGuaranteeFee'], currency)
    lines: list[FeeLine] = []
    if fee_type == 'OUR':
        lines.append(FeeLine(who='sender', label=sf_label, amount=sf))
        lines.append(FeeLine(who='sender', label='Доплата за гарантию полной суммы: комиссии банка-посредника и банка '
                                                 'получателя оплачиваете вы, поставщик получит всю сумму', amount=guar))
        sender_fees, withheld = sf + guar, 0.0
    elif fee_type == 'SHA':
        lines.append(FeeLine(who='sender', label=sf_label, amount=sf))
        lines.append(FeeLine(who='beneficiary', label='Комиссия банка-посредника, её удержат из суммы перевода',
                             amount=inter))
        lines.append(FeeLine(who='beneficiary', label='Комиссия банка получателя, её удержат из суммы перевода',
                             amount=benf))
        sender_fees, withheld = sf, inter + benf
    else:
        fee_type = 'BEN'
        lines.append(FeeLine(who='beneficiary', label='Комиссия нашего банка, её удержат из суммы перевода', amount=sf))
        lines.append(FeeLine(who='beneficiary', label='Комиссия банка-посредника, её удержат из суммы перевода',
                             amount=inter))
        lines.append(FeeLine(who='beneficiary', label='Комиссия банка получателя, её удержат из суммы перевода',
                             amount=benf))
        sender_fees, withheld = 0.0, sf + inter + benf
    withheld = round(min(withheld, amount), 2)
    sender_fees = round(sender_fees, 2)
    return FeeSim(fee_type=fee_type, currency=currency, sender_total=round(amount + sender_fees, 2),
                  sender_fees=sender_fees, receiver_gets=round(amount - withheld, 2), shortfall=withheld,
                  urgent_fee_kzt=float(t['urgentFeeKzt']) if urgent else 0.0, lines=lines)
