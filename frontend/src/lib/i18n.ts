export type Locale = "ar" | "en";

/** Cookie read server-side by the root layout to set <html dir/lang>. */
export const LOCALE_COOKIE = "birrcare_lang";

export const translations = {
  ar: {
    appName: "رعاية بر",
    appSubtitle: "منصة التنسيق العائلي لرعاية مريض الجلطة",
    birrDescription: "بِرّ الوالدين — تعاون الأبناء لحظة بلحظة",
    
    // Nav
    navMeds: "الأدوية",
    navVitals: "المؤشرات الحيوية",
    navHandovers: "تسليم الوردية",
    navDoctorExport: "تقرير الطبيب",

    // Caregiver
    activeCaregiver: "المسؤول الحالي",
    switchCaregiver: "تبديل المسؤول",
    enterPin: "أدخل رمز المرور (4 أرقام)",
    incorrectPin: "رمز المرور غير صحيح",
    selectProfile: "اختر اسمك للبدء",
    pinHint: "رمز المرور الافتراضي: 1234",
    confirm: "تأكيد",
    cancel: "إلغاء",
    userManagement: "إدارة المستخدمين",
    manageMedications: "إدارة الأدوية والجرعات",
    readOnlyNotice: "وضع القراءة فقط: حساب المشاهد مخصص للمتابعة بدون إمكانية تسجيل أو تعديل البيانات.",
    roles: {
      super_admin: "مشرف عام",
      admin: "مدير أدوية",
      caregiver: "مقدم رعاية",
      viewer: "مشاهد (قراءة فقط)",
    },

    // Meds
    todayMedications: "جدول أدوية اليوم",
    slots: {
      morning: "الصباح",
      afternoon: "الظهيرة",
      evening: "المساء",
      bedtime: "قبل النوم",
    },
    markTaken: "تسجيل أخذ الجرعة",
    markSkipped: "تم التخطي",
    saving: "جارٍ الحفظ...",
    givenBy: "أُعطيت بواسطة",
    atTime: "في",
    statusTaken: "تم الإعطاء بنجاح",
    statusSkipped: "جرعة متخطاة",
    notesOptional: "ملاحظات (اختياري)",
    doubleDoseAlert: "تنبيه أمان: تم تسجيل هذه الجرعة مسبقاً!",

    // Vitals
    vitalsTitle: "مؤشرات التعافي من الجلطة",
    recordVitals: "تسجيل قراءة جديدة",
    chooseVitalType: "اختر نوع الفحص المراد تسجيله",
    chooseVitalTypeSubtitle: "سجّل الفحص الذي تم قياسه الآن دون الحاجة لتعبئة باقي الفحوصات",
    vitalBPTitle: "ضغط الدم والنبض",
    vitalBPDesc: "تسجيل قراءة الضغط الانقباضي والانبساطي ونبضات القلب",
    vitalSugarTitle: "فحص سكر الدم",
    vitalSugarDesc: "تسجيل قياس السكر بوحدة mmol/L (صائم / بعد الأكل / عشوائي)",
    vitalAlertnessTitle: "الحالة الإدراكية والوعي",
    vitalAlertnessDesc: "تقييم اليقظة، الاستجابة، والتشتت العصبي بعد الجلطة",
    vitalFullTitle: "فحص دوري شامل",
    vitalFullDesc: "تسجيل الضغط والنبض والسكر ومستوى الوعي معاً في جلسة واحدة",
    changeVitalType: "تغيير نوع الفحص",
    systolic: "الضغط الانقباضي",
    diastolic: "الضغط الانبساطي",
    pulse: "النبض (bpm)",
    bloodSugar: "سكر الدم (mmol/L)",
    alertnessLevel: "مستوى الوعي واليقظة",
    vitalsHistory: "سجل القراءات السابقة",
    saveVitals: "حفظ القراءات",
    latestReading: "آخر قياس",

    // Handovers
    handoversTitle: "ملاحظات تسليم الوردية",
    newHandover: "إضافة ملاحظة جديدة",
    shiftType: "فترة الوردية",
    shifts: {
      morning: "الوردية الصباحية",
      evening: "الوردية المسائية",
      night: "الوردية الليلية",
    },
    urgentFlag: "تنبيه هام ومستعجل للأخوة",
    summaryPlaceholder: "اكتب ما حدث في الوردية (الشهية، المزاج، الحركة، المواعيد)...",
    postNote: "نشر الملاحظة للعائلة",

    // Doctor Export
    doctorExportTitle: "ملخص الاستشارة الطبية",
    doctorExportSubtitle: "تقرير شامل ومطبوع موجه لطبيب الأعصاب والباطنية",
    printReport: "طباعة التقرير / تصدير PDF",
    coveredPeriod: "الفترة المشمولة",
    adherenceRate: "معدل الالتزام بالأدوية",
    bpOverview: "نظرة عامة على ضغط الدم",
    cognitiveDistribution: "توزيع الحالة الإدراكية",
    urgentEvents: "الملاحظات الحرجة",
    doctorNotesPlaceholder: "مساحة ملاحظات وتوجيهات الطبيب...",

    // Realtime Sync
    liveConnected: "متصل لحظياً",
    liveReconnecting: "جارٍ إعادة الاتصال...",
    liveOffline: "غير متصل",
    toastDoseTaken: "سجل جرعة",
    toastVitalsRecorded: "سجل قراءة حيوية جديدة",
    toastHandoverAdded: "أضاف ملاحظة تسليم وردية",
  },
  en: {
    appName: "Birrcare",
    appSubtitle: "Family Care Coordination Platform for Stroke Recovery",
    birrDescription: "Birr al-Walidayn — Real-time sibling care collaboration",

    // Nav
    navMeds: "Medications",
    navVitals: "Stroke Vitals",
    navHandovers: "Handovers",
    navDoctorExport: "Doctor Summary",

    // Caregiver
    activeCaregiver: "Active Caregiver",
    switchCaregiver: "Switch Profile",
    enterPin: "Enter 4-Digit PIN",
    incorrectPin: "Incorrect PIN code",
    selectProfile: "Select your profile to start",
    pinHint: "Default PIN: 1234",
    confirm: "Confirm",
    cancel: "Cancel",
    userManagement: "User Management",
    manageMedications: "Prescriptions Catalog",
    readOnlyNotice: "View-only mode: This profile is authorized for monitoring only without write access.",
    roles: {
      super_admin: "Super Admin",
      admin: "Admin",
      caregiver: "Caregiver",
      viewer: "Viewer (Read Only)",
    },

    // Meds
    todayMedications: "Today's Medication Schedule",
    slots: {
      morning: "Morning",
      afternoon: "Afternoon",
      evening: "Evening",
      bedtime: "Bedtime",
    },
    markTaken: "Mark as Taken",
    markSkipped: "Mark as Skipped",
    saving: "Saving...",
    givenBy: "Given by",
    atTime: "at",
    statusTaken: "Given Successfully",
    statusSkipped: "Dose Skipped",
    notesOptional: "Notes (Optional)",
    doubleDoseAlert: "Safety Alert: This dose was already logged!",

    // Vitals
    vitalsTitle: "Stroke Recovery Vitals",
    recordVitals: "Record New Vitals",
    chooseVitalType: "Choose Vital Metric to Record",
    chooseVitalTypeSubtitle: "Record only the metric measured right now without needing to fill all fields",
    vitalBPTitle: "Blood Pressure & Pulse",
    vitalBPDesc: "Record systolic & diastolic pressure and heart rate",
    vitalSugarTitle: "Blood Glucose (Sugar)",
    vitalSugarDesc: "Record glucose level in mmol/L with meal timing",
    vitalAlertnessTitle: "Cognitive Alertness Level",
    vitalAlertnessDesc: "Assess mental alertness, responsiveness, or confusion",
    vitalFullTitle: "Full Comprehensive Checkup",
    vitalFullDesc: "Log BP, pulse, glucose, and alertness all in one session",
    changeVitalType: "Change Vital Type",
    systolic: "Systolic (mmHg)",
    diastolic: "Diastolic (mmHg)",
    pulse: "Pulse (bpm)",
    bloodSugar: "Blood Sugar (mmol/L)",
    alertnessLevel: "Cognitive Alertness Level",
    vitalsHistory: "Vitals History",
    saveVitals: "Save Vitals",
    latestReading: "Latest Reading",

    // Handovers
    handoversTitle: "Shift Handover Notes",
    newHandover: "Add Handover Note",
    shiftType: "Shift Window",
    shifts: {
      morning: "Morning Shift",
      evening: "Evening Shift",
      night: "Night Shift",
    },
    urgentFlag: "Urgent Sibling Notice",
    summaryPlaceholder: "Describe patient status (appetite, mood, mobility, appointments)...",
    postNote: "Post Handover Note",

    // Doctor Export
    doctorExportTitle: "Doctor Consultation Summary",
    doctorExportSubtitle: "Comprehensive clinical report for neurologist & physician",
    printReport: "Print Summary / Export PDF",
    coveredPeriod: "Covered Period",
    adherenceRate: "Medication Adherence Rate",
    bpOverview: "Blood Pressure Overview",
    cognitiveDistribution: "Cognitive State Distribution",
    urgentEvents: "Urgent Alerts & Handover Notices",
    doctorNotesPlaceholder: "Doctor's examination notes & recommendations...",

    // Realtime Sync
    liveConnected: "Live Sync",
    liveReconnecting: "Reconnecting...",
    liveOffline: "Offline",
    toastDoseTaken: "logged medication dose",
    toastVitalsRecorded: "recorded new stroke vitals",
    toastHandoverAdded: "posted a handover note",
  },
};
