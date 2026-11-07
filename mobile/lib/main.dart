import 'dart:convert';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:shared_preferences/shared_preferences.dart';

const ink = Color(0xFF1D232B);
const accent = Color(0xFF174E4A);
const paper = Color(0xFFF4F2EE);
const line = Color(0xFFDCD8D0);

void main() => runApp(const BccFxApp());

class BccFxApp extends StatefulWidget {
  const BccFxApp({super.key});
  @override
  State<BccFxApp> createState() => _BccFxAppState();
}

class _BccFxAppState extends State<BccFxApp> {
  late String _serverUrl = defaultServerUrl;
  int _tab = 1;

  @override
  void initState() {
    super.initState();
    SharedPreferences.getInstance().then((prefs) {
      final saved = prefs.getString('serverUrl');
      if (saved != null && mounted) setState(() => _serverUrl = saved);
    });
  }

  void _saveServerUrl(String value) {
    setState(() => _serverUrl = value);
    SharedPreferences.getInstance().then((prefs) => prefs.setString('serverUrl', value));
  }

  String get defaultServerUrl =>
      !kIsWeb && defaultTargetPlatform == TargetPlatform.android
          ? 'http://10.0.2.2:8080'
          : 'http://localhost:8080';

  ApiClient get api => ApiClient(_serverUrl);

  @override
  Widget build(BuildContext context) {
    final pages = <Widget>[
      DashboardPage(onOpenOperations: () => setState(() => _tab = 1)),
      OperationsPage(api: api, onServerChanged: _saveServerUrl),
      ServicesPage(api: api),
      const MessagesPage(),
      SettingsPage(serverUrl: _serverUrl, onServerChanged: _saveServerUrl),
    ];
    return MaterialApp(
      debugShowCheckedModeBanner: false,
      title: 'ВЭД-помощник',
      theme: ThemeData(
        useMaterial3: true,
        scaffoldBackgroundColor: paper,
        colorScheme: ColorScheme.fromSeed(seedColor: accent, surface: Colors.white),
        appBarTheme: const AppBarTheme(backgroundColor: paper, foregroundColor: ink, elevation: 0),
        inputDecorationTheme: InputDecorationTheme(
          filled: true, fillColor: Colors.white,
          border: OutlineInputBorder(borderRadius: BorderRadius.circular(12), borderSide: const BorderSide(color: line)),
          enabledBorder: OutlineInputBorder(borderRadius: BorderRadius.circular(12), borderSide: const BorderSide(color: line)),
          contentPadding: const EdgeInsets.symmetric(horizontal: 14, vertical: 14),
        ),
      ),
      home: Scaffold(
        body: SafeArea(child: IndexedStack(index: _tab, children: pages)),
        bottomNavigationBar: NavigationBar(
          selectedIndex: _tab,
          onDestinationSelected: (i) => setState(() => _tab = i),
          destinations: const [
            NavigationDestination(icon: Icon(Icons.home_outlined), selectedIcon: Icon(Icons.home), label: 'Бизнес'),
            NavigationDestination(icon: Icon(Icons.swap_horiz), label: 'Операции'),
            NavigationDestination(icon: Icon(Icons.grid_view_outlined), label: 'Сервисы'),
            NavigationDestination(icon: Icon(Icons.chat_bubble_outline), label: 'Сообщения'),
            NavigationDestination(icon: Icon(Icons.more_horiz), label: 'Ещё'),
          ],
        ),
      ),
    );
  }
}

class ApiClient {
  ApiClient(this.baseUrl);
  final String baseUrl;

  Future<Map<String, dynamic>> demo() async {
    final response = await http.get(Uri.parse('$baseUrl/api/demo')).timeout(const Duration(seconds: 30));
    return _decode(response);
  }

  Future<Map<String, dynamic>> explain(String ruleId) async {
    final response = await http.post(Uri.parse('$baseUrl/api/ai/explain'),
      headers: const {'Content-Type': 'application/json'},
      body: jsonEncode({'ruleId': ruleId})).timeout(const Duration(seconds: 30));
    return _decode(response);
  }

  Future<Map<String, dynamic>> analyze(List<PlatformFile> files, Map<String, String> fields) async {
    final request = http.MultipartRequest('POST', Uri.parse('$baseUrl/api/analyze'));
    request.fields.addAll(fields);
    for (final file in files) {
      final bytes = file.bytes;
      if (bytes == null) throw Exception('Не удалось прочитать ${file.name}. Выберите файл ещё раз.');
      request.files.add(http.MultipartFile.fromBytes('documents', bytes, filename: file.name));
    }
    final streamed = await request.send().timeout(const Duration(seconds: 90));
    final response = await http.Response.fromStream(streamed);
    return _decode(response);
  }

  Map<String, dynamic> _decode(http.Response response) {
    final decoded = jsonDecode(response.body);
    if (response.statusCode < 200 || response.statusCode >= 300) {
      throw Exception(decoded is Map ? decoded['error'] ?? 'Ошибка сервера' : 'Ошибка сервера');
    }
    return Map<String, dynamic>.from(decoded as Map);
  }
}

class DashboardPage extends StatelessWidget {
  const DashboardPage({super.key, required this.onOpenOperations});
  final VoidCallback onOpenOperations;
  @override
  Widget build(BuildContext context) => ListView(padding: const EdgeInsets.all(20), children: [
    const _BrandHeader(),
    const SizedBox(height: 22),
    const Text('Добрый день', style: TextStyle(fontSize: 25, fontWeight: FontWeight.w700, color: ink)),
    const SizedBox(height: 8),
    const Text('Ваши валютные операции и документы — в одном месте.', style: TextStyle(color: Color(0xFF68717B))),
    const SizedBox(height: 18),
    Card(child: Padding(padding: const EdgeInsets.all(18), child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
      const Row(children: [Icon(Icons.description_outlined, color: accent), SizedBox(width: 10), Expanded(child: Text('Документы на проверку', style: TextStyle(fontWeight: FontWeight.w600))), Icon(Icons.chevron_right)]),
      const SizedBox(height: 14),
      const Text('Загрузите договор и инвойс — помощник сверит реквизиты и условия.'),
      const SizedBox(height: 14),
      FilledButton(onPressed: onOpenOperations, child: const Text('Перейти к операциям')),
    ]))),
    const SizedBox(height: 12),
    const _SectionTitle('Переводы и платежи'),
    _ActionTile(icon: Icons.language, title: 'Валютные переводы', subtitle: 'Клиенту банка или в другой банк', onTap: onOpenOperations),
    _ActionTile(icon: Icons.account_balance_outlined, title: 'Валютные договоры', subtitle: 'Проверка документов и учётный номер', onTap: onOpenOperations),
  ]);
}

class OperationsPage extends StatelessWidget {
  const OperationsPage({super.key, required this.api, required this.onServerChanged});
  final ApiClient api;
  final ValueChanged<String> onServerChanged;
  @override
  Widget build(BuildContext context) => ListView(padding: const EdgeInsets.all(20), children: [
    const _BrandHeader(), const SizedBox(height: 18),
    const Text('Операции', style: TextStyle(fontSize: 25, fontWeight: FontWeight.w700, color: ink)),
    const SizedBox(height: 16),
    Card(child: ListTile(leading: const Icon(Icons.draw_outlined, color: accent), title: const Text('Документы на проверку'), subtitle: const Text('Загрузить договор или инвойс'), trailing: const Icon(Icons.chevron_right), onTap: () => _openAnalyzer(context, api, title: 'Проверка документов'))),
    const SizedBox(height: 18), const _SectionTitle('Переводы и платежи'),
    _ActionTile(icon: Icons.person_outline, title: 'Контрагенту', subtitle: 'Оплата по реквизитам', onTap: () => Navigator.push(context, MaterialPageRoute(builder: (_) => TransfersPage(api: api)))),
    const _ActionTile(icon: Icons.account_balance_wallet_outlined, title: 'Между своими счетами', subtitle: 'Внутри одной организации'),
    const _ActionTile(icon: Icons.schedule, title: 'Алименты', subtitle: 'Оплата по реквизитам'),
    const SizedBox(height: 12), const _SectionTitle('Валютные операции'),
    _ActionTile(icon: Icons.folder_open_outlined, title: 'Валютные договоры', subtitle: 'Документы и учётный номер', onTap: () => _openAnalyzer(context, api, title: 'Валютные договоры')),
    _ActionTile(icon: Icons.language, title: 'Валютные переводы', subtitle: 'Клиенту банка или в другой банк', onTap: () => Navigator.push(context, MaterialPageRoute(builder: (_) => TransfersPage(api: api)))),
    const _ActionTile(icon: Icons.currency_exchange, title: 'Конвертация', subtitle: 'По курсу рынка'),
    const SizedBox(height: 12),
    Card(child: ListTile(leading: const Icon(Icons.settings_outlined), title: const Text('Адрес сервера'), subtitle: const Text('Настройка подключения к backend'), onTap: () => Navigator.push(context, MaterialPageRoute(builder: (_) => SettingsPage(serverUrl: api.baseUrl, onServerChanged: onServerChanged))))),
  ]);
}

class TransfersPage extends StatelessWidget {
  const TransfersPage({super.key, required this.api});
  final ApiClient api;
  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('Валютные переводы')),
    floatingActionButton: FloatingActionButton.extended(onPressed: () => Navigator.push(context, MaterialPageRoute(builder: (_) => TransferFormPage(api: api))), icon: const Icon(Icons.add), label: const Text('Новая заявка')),
    body: ListView(padding: const EdgeInsets.all(16), children: [
      const Text('Выберите тип перевода', style: TextStyle(fontWeight: FontWeight.w700, fontSize: 18)),
      const SizedBox(height: 10),
      Card(child: Column(children: [ListTile(leading: const Icon(Icons.account_balance_outlined), title: const Text('В другой банк'), trailing: const Icon(Icons.chevron_right), onTap: () => Navigator.push(context, MaterialPageRoute(builder: (_) => TransferFormPage(api: api)))), const Divider(height: 1), const ListTile(leading: Icon(Icons.account_balance), title: Text('Внутри банка'), subtitle: Text('Скоро появятся в приложении'))])),
      const SizedBox(height: 18), const Text('Заявки', style: TextStyle(fontWeight: FontWeight.w700, fontSize: 18)),
      const SizedBox(height: 8),
      const _RequestTile(name: 'ИП «Speed8»', amount: '10 000 USD', status: 'Создано', color: Color(0xFF68717B)),
      const _RequestTile(name: 'ИП «Speed8»', amount: '10 000 USD', status: 'В обработке', color: Color(0xFF2765C5)),
      const _RequestTile(name: 'ИП «Speed8»', amount: '10 000 USD', status: 'Отклонено', color: Color(0xFFA3261B)),
      const SizedBox(height: 12),
      OutlinedButton.icon(onPressed: () => _showDemo(context, api), icon: const Icon(Icons.science_outlined), label: const Text('Проверить демонстрационный комплект')),
    ]),
  );
}

class TransferFormPage extends StatefulWidget {
  const TransferFormPage({super.key, required this.api});
  final ApiClient api;
  @override
  State<TransferFormPage> createState() => _TransferFormPageState();
}

class _TransferFormPageState extends State<TransferFormPage> {
  final _controllers = <String, TextEditingController>{};
  final List<PlatformFile> _files = [];
  bool _acceptTerms = false, _busy = false;
  String? _error;
  Map<String, dynamic>? _analysis;

  @override
  void dispose() { for(final c in _controllers.values){c.dispose();} super.dispose(); }

  Future<void> _pick() async {
    final result = await FilePicker.platform.pickFiles(allowMultiple: true, withData: true, type: FileType.custom, allowedExtensions: const ['pdf','docx','txt']);
    if(result!=null) setState(()=>_files.addAll(result.files));
  }

  Future<void> _check() async {
    if(_files.isEmpty){setState(()=>_error='Добавьте договор или инвойс для проверки.');return;}
    setState(()=>_busy=true);
    try {
      final form=<String,dynamic>{};
      for(final entry in _controllers.entries){final value=entry.value.text.trim();if(value.isNotEmpty){form[entry.key]={'value':value,'status':'user','confirmed':true};}}
      if(_acceptTerms){form['acceptTerms']={'value':'true','status':'user','confirmed':true};}
      final result=await widget.api.analyze(_files,{'form':jsonEncode(form),'task':jsonEncode({'timing':'prepay'})});
      setState(()=>_analysis=result);
    } catch(e){setState(()=>_error=e.toString().replaceFirst('Exception: ',''));}
    finally{if(mounted)setState(()=>_busy=false);}
  }

  void _sign(){if(!_acceptTerms)return;showDialog<void>(context:context,builder:(context)=>AlertDialog(title:const Text('Подписание'),content:const Text('Это демонстрационный экран. Для отправки и электронной подписи требуется подключить банковский процесс.'),actions:[TextButton(onPressed:()=>Navigator.pop(context),child:const Text('Понятно'))]));}

  @override
  Widget build(BuildContext context) {
    final sections=<String,List<(String,String)>>{
      'Перевод': [('requestNo','Номер заявки'),('debitAccount','Счёт списания'),('amount','Сумма перевода'),('valueDate','Дата валютирования'),('urgent','Срочный перевод (да/нет)')],
      'Банк получателя': [('beneficiarySwift','SWIFT банка получателя'),('intermediaryBank','Банк-посредник'),('knp','Код назначения платежа'),('purpose','Назначение перевода'),('linkContract','Учётный номер договора')],
      'Документы': [('docNumber','Номер документа'),('docDate','Дата документа'),('attachments','Описание вложений')],
      'Получатель': [('beneficiaryName','Наименование получателя'),('residenceCountry','Страна резидентства'),('beneficiaryIban','IBAN получателя'),('kbe','Код бенефициара'),('beneficiaryCountry','Страна получателя'),('addressType','Тип адреса')],
      'Комиссия': [('feeType','Тип комиссии (OUR / SHA / BEN)'),('feeAccount','Счёт комиссии')],
      'Подписание': [('signerHead','Руководитель'),('signerAccountant','Главный бухгалтер'),('useBonuses','Использовать бонусы (да/нет)')],
    };
    return Scaffold(
      appBar: AppBar(title: const Text('Новая заявка')),
      body: ListView(padding: const EdgeInsets.fromLTRB(16,12,16,24), children: [
        const Text('Перевод в другой банк',style:TextStyle(fontSize:22,fontWeight:FontWeight.w700,color:ink)),
        const SizedBox(height:6), const Text('Поля из документов можно проверить до отправки на подпись.'),
        const SizedBox(height:14),
        Card(child: Padding(padding:const EdgeInsets.all(16),child:Column(crossAxisAlignment:CrossAxisAlignment.start,children:[
          const Text('Документы',style:TextStyle(fontWeight:FontWeight.w700,fontSize:17)),const SizedBox(height:6),
          const Text('PDF, DOCX или TXT • до 15 МБ каждый'),const SizedBox(height:12),
          OutlinedButton.icon(onPressed:_pick,icon:const Icon(Icons.upload_file),label:const Text('Добавить файлы')),
          for(final file in _files) ListTile(dense:true,contentPadding:EdgeInsets.zero,leading:const Icon(Icons.description_outlined),title:Text(file.name,maxLines:1,overflow:TextOverflow.ellipsis),trailing:IconButton(onPressed:()=>setState(()=>_files.remove(file)),icon:const Icon(Icons.close))),
          if(_files.isEmpty) const Text('Пока нет загруженных документов',style:TextStyle(color:Colors.black54)),
        ]))),
        for(final section in sections.entries) ...[
          const SizedBox(height:12), _SectionTitle(section.key),
          for(final (id,label) in section.value) _formField(id,label),
        ],
        const SizedBox(height:10),
        Card(child: CheckboxListTile(value:_acceptTerms,onChanged:(v)=>setState(()=>_acceptTerms=v??false),controlAffinity:ListTileControlAffinity.leading,title:const Text('Принимаю условия соглашения и отказ от претензий'),subtitle:const Text('Отметку ставит человек. Без неё подписание недоступно.'))),
        if(_error!=null) Padding(padding:const EdgeInsets.symmetric(vertical:10),child:Text(_error!,style:const TextStyle(color:Color(0xFFA3261B)))),
        const SizedBox(height:8),
        FilledButton.icon(onPressed:_busy?null:_check,icon:_busy?const SizedBox(width:18,height:18,child:CircularProgressIndicator(strokeWidth:2)):const Icon(Icons.fact_check_outlined),label:Text(_busy?'Проверяем документы':'Проверить документы')),
        const SizedBox(height:8),
        FilledButton(onPressed:_acceptTerms?_sign:null,child:const Text('Подписать')),
        if(_analysis!=null) ...[_FindingsView(analysis:_analysis!,api:widget.api,onSource:_showSource),const SizedBox(height:20)],
      ]),
    );
  }

  Widget _formField(String id,String label){
    final controller=_controllers.putIfAbsent(id,TextEditingController.new);
    return Padding(padding:const EdgeInsets.only(bottom:10),child:TextField(controller:controller,decoration:InputDecoration(labelText:label,isDense:true)));
  }

  void _showSource(Map<String,dynamic> finding){
    _showFinding(context,widget.api,finding);
  }
}

class DocumentAnalyzerPage extends StatefulWidget {
  const DocumentAnalyzerPage({super.key,required this.api,required this.title});
  final ApiClient api;final String title;
  @override
  State<DocumentAnalyzerPage> createState()=>_DocumentAnalyzerPageState();
}

class _DocumentAnalyzerPageState extends State<DocumentAnalyzerPage>{
  List<PlatformFile> files=[];Map<String,dynamic>? analysis;String? error;bool busy=false;
  Future<void> pick()async{final r=await FilePicker.platform.pickFiles(allowMultiple:true,withData:true,type:FileType.custom,allowedExtensions:const['pdf','docx','txt']);if(r!=null)setState(()=>files.addAll(r.files));}
  Future<void> check()async{if(files.isEmpty){setState(()=>error='Сначала добавьте документы.');return;}setState(()=>busy=true);try{analysis=await widget.api.analyze(files,{});error=null;}catch(e){error=e.toString().replaceFirst('Exception: ','');}finally{if(mounted)setState(()=>busy=false);}}
  @override Widget build(BuildContext context)=>Scaffold(appBar:AppBar(title:Text(widget.title)),body:ListView(padding:const EdgeInsets.all(16),children:[const Text('Загрузите договор, инвойс или письмо поставщика.',style:TextStyle(fontSize:16)),const SizedBox(height:14),OutlinedButton.icon(onPressed:pick,icon:const Icon(Icons.upload_file),label:const Text('Выбрать документы')),for(final f in files)ListTile(leading:const Icon(Icons.description),title:Text(f.name),trailing:IconButton(onPressed:()=>setState(()=>files.remove(f)),icon:const Icon(Icons.close))),if(error!=null)Text(error!,style:const TextStyle(color:Colors.red)),FilledButton(onPressed:busy?null:check,child:Text(busy?'Проверяем…':'Проверить')),if(analysis!=null)_FindingsView(analysis:analysis!,api:widget.api,onSource:(f)=>_showFinding(context,widget.api,f))]));
}

void _showFinding(BuildContext context, ApiClient api, Map<String, dynamic> finding) {
  showModalBottomSheet<void>(
    context: context,
    showDragHandle: true,
    builder: (_) => _FindingDetails(api:api, finding:finding),
  );
}

class _FindingDetails extends StatefulWidget {
  const _FindingDetails({required this.api, required this.finding});
  final ApiClient api;
  final Map<String,dynamic> finding;
  @override State<_FindingDetails> createState()=>_FindingDetailsState();
}
class _FindingDetailsState extends State<_FindingDetails> {
  bool busy=false; String? explanation; String? source; String? error;
  Future<void> explain() async {
    setState(() { busy=true; error=null; });
    try { final result=await widget.api.explain(widget.finding['rule']?.toString()??''); if(mounted)setState(() { explanation=result['explanation']?.toString()??''; source=result['source']?.toString(); }); }
    catch(e){if(mounted)setState(()=>error=e.toString().replaceFirst('Exception: ',''));}
    finally{if(mounted)setState(()=>busy=false);}
  }
  @override Widget build(BuildContext context)=>Padding(padding:const EdgeInsets.all(20),child:SingleChildScrollView(child:Column(mainAxisSize:MainAxisSize.min,crossAxisAlignment:CrossAxisAlignment.start,children:[Text(widget.finding['title']?.toString()??'Проверка',style:const TextStyle(fontSize:19,fontWeight:FontWeight.w700)),const SizedBox(height:10),Text(widget.finding['detail']?.toString()??''),for(final item in (widget.finding['sources'] as List? ?? const[]))if(item is Map&&(item['quote']??'').toString().isNotEmpty)Padding(padding:const EdgeInsets.only(top:12),child:Text('«${item['quote']}»',style:const TextStyle(fontStyle:FontStyle.italic))),const SizedBox(height:12),OutlinedButton.icon(onPressed:busy?null:explain,icon:const Icon(Icons.help_outline),label:Text(busy?'Готовим пояснение…':'Почему?')),if(explanation!=null)Padding(padding:const EdgeInsets.only(top:8),child:Text('$explanation\n\n${source=='deepseek'?'Пояснение DeepSeek':'Запасное пояснение'}')),if(error!=null)Padding(padding:const EdgeInsets.only(top:8),child:Text(error!,style:const TextStyle(color:Colors.red)))])));
}

class _FindingsView extends StatelessWidget {
  const _FindingsView({required this.analysis,required this.api,required this.onSource});
  final Map<String,dynamic> analysis;final ApiClient api;final ValueChanged<Map<String,dynamic>> onSource;
  @override Widget build(BuildContext context){final report=Map<String,dynamic>.from(analysis['paymentReport'] as Map? ?? const{});final findings=(report['findings'] as List? ?? const[]).whereType<Map>().map((e)=>Map<String,dynamic>.from(e)).where((f)=>f['severity']!='green').toList();return Column(crossAxisAlignment:CrossAxisAlignment.start,children:[const SizedBox(height:18),const Text('Результат проверки',style:TextStyle(fontSize:20,fontWeight:FontWeight.w700)),if(findings.isEmpty)const Card(child:ListTile(leading:Icon(Icons.check_circle,color:Color(0xFF2F6B3A)),title:Text('Критичных замечаний не найдено'))),for(final f in findings)Card(child:ListTile(onTap:()=>onSource(f),leading:Icon(f['severity']=='red'?Icons.error_outline:Icons.warning_amber_rounded,color:f['severity']=='red'?const Color(0xFFA3261B):const Color(0xFF8A5A00)),title:Text(f['title']?.toString()??f['rule']?.toString()??'Проверка'),subtitle:Text(f['detail']?.toString()??'',maxLines:3,overflow:TextOverflow.ellipsis),trailing:const Icon(Icons.chevron_right))),]);}
}

class SettingsPage extends StatefulWidget {
  const SettingsPage({super.key,required this.serverUrl,required this.onServerChanged});
  final String serverUrl;final ValueChanged<String> onServerChanged;
  @override State<SettingsPage> createState()=>_SettingsPageState();
}
class _SettingsPageState extends State<SettingsPage>{late final TextEditingController controller=TextEditingController(text:widget.serverUrl);String? status;bool busy=false;
  @override void dispose(){controller.dispose();super.dispose();}
  Future<void> test()async{setState(()=>busy=true);try{final response=await http.get(Uri.parse('${controller.text.trim()}/health')).timeout(const Duration(seconds:5));if(response.statusCode!=200)throw Exception('HTTP ${response.statusCode}');widget.onServerChanged(controller.text.trim());setState(()=>status='Подключение работает');}catch(_){setState(()=>status='Не удалось подключиться. Проверьте адрес и запущенный backend.');}finally{if(mounted)setState(()=>busy=false);}}
  @override Widget build(BuildContext context)=>Scaffold(appBar:AppBar(title:const Text('Настройки сервера')),body:ListView(padding:const EdgeInsets.all(20),children:[const Text('Адрес Go API',style:TextStyle(fontSize:20,fontWeight:FontWeight.w700)),const SizedBox(height:8),const Text('Android Emulator: http://10.0.2.2:8080. Браузер или iOS Simulator: http://localhost:8080. Для реального телефона укажите IP компьютера в той же сети.'),const SizedBox(height:16),TextField(controller:controller,keyboardType:TextInputType.url,decoration:const InputDecoration(labelText:'URL сервера',hintText:'http://localhost:8080')),const SizedBox(height:12),FilledButton(onPressed:busy?null:test,child:Text(busy?'Проверяем…':'Проверить и сохранить')),if(status!=null)Padding(padding:const EdgeInsets.only(top:12),child:Text(status!,style:TextStyle(color:status=='Подключение работает'?const Color(0xFF2F6B3A):const Color(0xFFA3261B))))]));
}

class ServicesPage extends StatelessWidget {
  const ServicesPage({super.key,required this.api});final ApiClient api;
  @override Widget build(BuildContext context)=>ListView(padding:const EdgeInsets.all(20),children:[const _BrandHeader(),const SizedBox(height:20),const Text('Сервисы',style:TextStyle(fontSize:25,fontWeight:FontWeight.w700)),_ActionTile(icon:Icons.science_outlined,title:'Как это работает',subtitle:'Проверка по правилам и документы с цитатами',onTap:()=>_showDemo(context,api)),const _ActionTile(icon:Icons.calculate_outlined,title:'Симулятор комиссии',subtitle:'Расчёт комиссий по тарифам'),const _ActionTile(icon:Icons.currency_exchange,title:'Конвертация',subtitle:'Валютные курсы'),const _ActionTile(icon:Icons.menu_book_outlined,title:'Законы РК',subtitle:'Правовые источники и ограничения')]);
}
class MessagesPage extends StatelessWidget {const MessagesPage({super.key});@override Widget build(BuildContext context)=>const Center(child:Padding(padding:EdgeInsets.all(24),child:Text('Сообщения и подсказки появятся здесь',textAlign:TextAlign.center)));}

class _BrandHeader extends StatelessWidget {const _BrandHeader();@override Widget build(BuildContext context)=>const Row(children:[CircleAvatar(backgroundColor:accent,child:Text('BCC',style:TextStyle(color:Colors.white,fontWeight:FontWeight.w700,fontSize:12))),SizedBox(width:10),Text('Business',style:TextStyle(fontWeight:FontWeight.w600,color:ink)),Spacer(),Icon(Icons.notifications_none,color:ink)]);}
class _SectionTitle extends StatelessWidget {const _SectionTitle(this.text);final String text;@override Widget build(BuildContext context)=>Padding(padding:const EdgeInsets.only(bottom:8),child:Text(text,style:const TextStyle(fontWeight:FontWeight.w700,color:ink,fontSize:16)));}
class _ActionTile extends StatelessWidget {const _ActionTile({required this.icon,required this.title,required this.subtitle,this.onTap});final IconData icon;final String title,subtitle;final VoidCallback? onTap;@override Widget build(BuildContext context)=>Card(child:ListTile(leading:Icon(icon,color:accent),title:Text(title,style:const TextStyle(fontWeight:FontWeight.w600)),subtitle:Text(subtitle),trailing:const Icon(Icons.chevron_right),onTap:onTap));}
class _RequestTile extends StatelessWidget {const _RequestTile({required this.name,required this.amount,required this.status,required this.color});final String name,amount,status;final Color color;@override Widget build(BuildContext context)=>Card(child:ListTile(title:Text(name),subtitle:Text('№123 • 12:30'),trailing:Column(mainAxisAlignment:MainAxisAlignment.center,crossAxisAlignment:CrossAxisAlignment.end,children:[Text(amount,style:const TextStyle(fontWeight:FontWeight.w600)),Text('● $status',style:TextStyle(color:color,fontSize:12))])));}

void _openAnalyzer(BuildContext context,ApiClient api,{required String title})=>Navigator.push(context,MaterialPageRoute(builder:(_)=>DocumentAnalyzerPage(api:api,title:title)));

Future<void> _showDemo(BuildContext context,ApiClient api)async{
  showDialog<void>(context:context,barrierDismissible:false,builder:(_)=>const Center(child:CircularProgressIndicator()));
  try{final data=await api.demo();if(context.mounted)Navigator.pop(context);if(context.mounted)Navigator.push(context,MaterialPageRoute(builder:(_)=>Scaffold(appBar:AppBar(title:const Text('Демонстрационная проверка')),body:ListView(padding:const EdgeInsets.all(16),children:[_FindingsView(analysis:data,api:api,onSource:(f)=>_showFinding(context,api,f))]))));}
  catch(e){if(context.mounted){Navigator.pop(context);ScaffoldMessenger.of(context).showSnackBar(SnackBar(content:Text('Не удалось связаться с backend: $e')));}}
}
