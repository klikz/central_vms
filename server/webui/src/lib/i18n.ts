// Bitta i18n fayli — barcha matn o'zbekcha (REJA §7 qabul mezoni: UI to'liq
// o'zbekcha). Kalitlar bo'limlar bo'yicha guruhlangan.
const uz: Record<string, string> = {
  "app.title": "Central VMS",
  "nav.monitor": "Kuzatuv",
  "nav.admin": "Boshqaruv",
  "nav.logout": "Chiqish",
  "nav.group.watch": "Kuzatuv",
  "nav.group.manage": "Boshqaruv",
  "side.user": "foydalanuvchi",
  "side.role": "rol",
  "side.hotkeys": "klaviatura",
  "side.cameras": "kameralar",
  "monitor.title": "Kuzatuv",
  "monitor.auto": "avto to'ldirish",
  "monitor.page": "sahifa",

  "login.title": "Kirish",
  "login.user": "Foydalanuvchi",
  "login.pass": "Parol",
  "login.submit": "Kirish",
  "login.bad_credentials": "Foydalanuvchi yoki parol xato",
  "login.rate_limited": "Juda ko'p urinish — bir daqiqa kuting",
  "login.network": "Server javob bermayapti",

  "side.sites": "Joylar",
  "side.clients": "Kompyuterlar",
  "side.all": "Barchasi",
  "side.online": "onlayn",
  "side.offline": "oflayn",

  "grid.empty": "Katak bo'sh — ro'yxatdan kamerani sudrab olib keling",
  "grid.loading": "yuklanmoqda…",
  "grid.transcoding": "transcode boshlandi…",
  "grid.offline": "kompyuter oflayn",
  "grid.cam_offline": "kamera oflayn",
  "grid.disabled": "kamera o'chirilgan",
  "grid.retry": "qayta urinish",
  "grid.error": "oqim xatosi",
  "grid.fullscreen": "butun ekran",

  "toast.limit_user": "Sizning sessiyalar limitiz to'ldi",
  "toast.limit_global": "Server sig'imi to'ldi — keyinroq qayta urinib ko'ring",
  "toast.limit_preview": "Preview limiti to'ldi",
  "toast.agent_offline": "Kompyuter oflayn — kamera ko'rinmaydi",
  "toast.saved": "Saqlandi",
  "toast.deleted": "O'chirildi",

  "fav.add": "Sevimlilarga qo'shish",
  "fav.remove": "Sevimlilardan olish",
  "fav.title": "Sevimlilar",

  "layout.save": "Layoutni saqlash",
  "layout.name": "Nomi",
  "layout.default": "Standart",
  "layout.title": "Layoutlar",

  "admin.clients": "Kompyuterlar",
  "admin.enroll": "Ulash kodi yaratish",
  "admin.enroll.note": "Kod bir marta ishlatiladi va 24 soat yashaydi",
  "admin.sessions": "Sessiyalar",
  "admin.users": "Foydalanuvchilar",
  "admin.audit": "Jurnal",
  "admin.transcode": "Transcode siyosati",
  "admin.rotate": "Maxfiy kodni almashtirish",
  "admin.refresh": "Yangilash",
  "admin.status": "holat",
  "admin.version": "versiya",
  "admin.camera": "kamera",
  "admin.state": "holat",
  "admin.expires": "muddat",
  "admin.stop": "to'xtatish",
  "admin.empty_clients": "Hali kompyuter ulanmagan — 'Ulash kodi yaratish' tugmasidan kod oling",
  "admin.delete": "O'chirish",

  "status.connected": "ulangan",
  "status.disconnected": "aloqa uzildi — qayta ulanilmoqda",
  "kbd.help": "1/2/3/4 — grid · ←/→ — sahifa · Esc — chiqish",
};

export function t(key: string): string {
  return uz[key] ?? key;
}
