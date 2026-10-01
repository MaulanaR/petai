const STR = {
  id: {
    chatPlaceholder: 'Ngobrol sama aku…',
    menuChat: '💬 Ajak ngobrol',
    menuSleep: '😴 Tidur',
    menuWake: '🌞 Bangun',
    menuMode: 'Gerak',
    modeStay: 'Diam',
    modeGround: 'Jalan',
    modeFree: 'Melayang',
    menuChar: 'Karakter',
    menuSettings: '⚙️ Pengaturan',
    menuHide: '🙈 Sembunyikan 1 jam',
    menuQuit: '✖ Keluar',
    noKey: 'Aku belum punya API key… atur di Pengaturan → AI ya 🔑',
    aiError: 'Hmm, otakku lagi error: ',
    watching: 'Aku lihat-lihat layarmu sebentar ya 👀',
    clickLines: ['Hehe, geli!', 'Eh? 👀', 'Halo~', 'Aku di sini!', 'Mau main?', 'Hihi', '✨', 'Jangan dicolek terus dong 😆'],
    petLines: ['Enaknya dielus~ 💕', 'Purr… eh, aku bukan kucing. Atau iya?', 'Lagi dong~'],
    wakeLines: ['Hoaam… eh kamu balik! 👋', 'Selamat datang lagi!'],
    dropLines: ['Wheee!', 'Hup!', 'Aduh, pelan-pelan 😵'],
  },
  en: {
    chatPlaceholder: 'Talk to me…',
    menuChat: '💬 Chat',
    menuSleep: '😴 Sleep',
    menuWake: '🌞 Wake up',
    menuMode: 'Move',
    modeStay: 'Stay',
    modeGround: 'Walk',
    modeFree: 'Float',
    menuChar: 'Character',
    menuSettings: '⚙️ Settings',
    menuHide: '🙈 Hide for 1 hour',
    menuQuit: '✖ Quit',
    noKey: "I don't have an API key yet… set it in Settings → AI 🔑",
    aiError: 'Hmm, my brain glitched: ',
    watching: "Peeking at your screen for a sec 👀",
    clickLines: ['Hehe, that tickles!', 'Huh? 👀', 'Hi there~', "I'm here!", 'Wanna play?', 'Hihi', '✨', 'Stop poking me 😆'],
    petLines: ['That feels nice~ 💕', 'More pats please~', 'Happy happy!'],
    wakeLines: ['Yawn… oh, you are back! 👋', 'Welcome back!'],
    dropLines: ['Wheee!', 'Hup!', 'Easy there 😵'],
  },
};

let lang = 'id';
export function setLang(l) { lang = STR[l] ? l : 'id'; }
export function t(key) { return STR[lang][key] ?? STR.id[key] ?? key; }
export function line(key) {
  const arr = STR[lang][key] || STR.id[key] || [''];
  return arr[Math.floor(Math.random() * arr.length)];
}
