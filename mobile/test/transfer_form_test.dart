import 'package:bcc_fx_assistant/main.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('Подписать disabled until a person accepts terms', (tester) async {
    await tester.pumpWidget(MaterialApp(home: TransferFormPage(api: ApiClient('http://localhost:8080'))));

    final terms = find.text('Принимаю условия соглашения и отказ от претензий');
    final scrollable = find.byType(Scrollable).first;
    await tester.dragUntilVisible(terms, scrollable, const Offset(0, -500));

    final signButton = find.widgetWithText(FilledButton, 'Подписать');
    await tester.dragUntilVisible(signButton, scrollable, const Offset(0, -350));
    expect(tester.widget<FilledButton>(signButton).onPressed, isNull);

    await tester.dragUntilVisible(terms, scrollable, const Offset(0, 350));
    await tester.tap(terms);
    await tester.pumpAndSettle();

    await tester.dragUntilVisible(signButton, scrollable, const Offset(0, -350));
    expect(tester.widget<FilledButton>(signButton).onPressed, isNotNull);
    await tester.tap(signButton);
    await tester.pumpAndSettle();
    expect(find.text('Это демонстрационный экран. Для отправки и электронной подписи требуется подключить банковский процесс.'), findsOneWidget);
  });
}
