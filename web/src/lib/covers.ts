// Позиционирование обложек статей (object/background-position).
// Единый источник для blog/index.astro (карточки) и blog/[slug].astro (шапка статьи).
// Значения могут отличаться: у карточки и полноразмерной обложки разный кроп.

export const coverPositionsCard: Record<string, string> = {
  'kak-nachat-trenirovatsya':                  'center 25%',
  'pochemu-brosaju-trenirovki':                'center 5%',
  'zaryadka':                                  'center center',
  'bolit-poyasnitsa-sidjachaja-rabota':        'center center',
  'pochemu-net-energii':                       'center 30%',
  'pochemu-ne-hudeu-hotja-treniruus':          'center 25%',
  'kak-uluchshit-son':                         'center 30%',
  'sustavnaja-gimnastika-dlja-nachinajuschih': 'center 25%',
  'kak-uluchshit-osanku':                      'center 40%',
  'kak-snizit-trevozhnost':                    'center 30%',
  'istorija-pereloma-pozvonochnika':           'center 15%',
  'kak-pravilno-podnimat-tyazhesti':           'center center',
  'mpk-chto-eto-i-kak-uluchshit':              'center 20%',
  'kak-sformirovat-privychku-trenirovatsya':   'center 25%',
  'kak-nachat-begat':                          'center 50%',
  'trenirovki-i-mentalnoe-zdorove':            'center 25%',
  'kak-pitatsya-chtoby-pohudjet':              'center 20%',
  'sotsialnoe-zdorove':                        'center 25%',
  'princip-malenkih-shagov':                   'center 30%',
  'uprazhnenija-ot-otekov':                    'center 65%',
  'gimnastika-dlja-glaz':                      'center 25%',
  'uprazhnenija-dlja-shei':                    'center 20%',
  'uprazhnenija-pri-sidjachej-rabote':         'center center',
  'kak-razvit-gibkost':                        'center 30%',
  'kak-begat-zimoj':                           'center 30%',
  'banja-i-zdorove':                           'center center',
  'kak-nauchitsja-meditirovat':                'center center',
  'akrojoga-chto-eto':                         'center center',
  'obuchenie-novomu-navyku':                   'center 15%',
  'dyhatelnye-tehniki':                        'center center',
  'kak-trenirujet-detej':                      'center center',
  'muzhskoe-zhenskoe-zdorove':                 'center center',
};

export const coverPositionsArticle: Record<string, string> = {
  ...coverPositionsCard,
  // отличия для полноразмерной обложки в статье
  'pochemu-brosaju-trenirovki': 'center 15%',
};
