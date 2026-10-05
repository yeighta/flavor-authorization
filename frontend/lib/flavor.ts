// Flavor families are inferred from the flavor name. The first family whose
// keyword appears wins, so the order below goes from most to least specific
// (e.g. "Mint Chocolate" reads as mint, "Blueberry Muffin" as dessert).

export type Family =
  | 'mint'
  | 'citrus'
  | 'berry'
  | 'tropical'
  | 'orchard'
  | 'dessert'
  | 'botanical'
  | 'other';

export const FAMILIES: { id: Family; label: string }[] = [
  { id: 'mint', label: 'ミント・クール' },
  { id: 'citrus', label: '柑橘' },
  { id: 'berry', label: 'ベリー' },
  { id: 'tropical', label: 'トロピカル' },
  { id: 'orchard', label: '果樹' },
  { id: 'dessert', label: 'スイーツ' },
  { id: 'botanical', label: 'ドリンク・花・スパイス' },
  { id: 'other', label: 'その他' },
];

const RULES: [Family, RegExp][] = [
  ['mint', /mint|menthol|\bice|\bicy\b|\bcool|frost|chill|polar|freez|arctic|glacier|snow|ミント|メンソール|アイス/i],
  ['dessert', /vanilla|chocolat|caramel|cake|cookie|biscuit|cream|milk|muffin|waffle|honey|candy|\bgum\b|cinnamon|pistachio|\bnuts?\b|almond|hazelnut|coffee|cappuccino|latte|espresso|donut|\bpie\b|pudding|sweet|sugar|toffee|marshmallow|cereal|yogurt|yoghourt|custard|brownie|butter|tiramisu|dessert|bubble|cotton|kaymak|halva|wafer|choco|skittles|horchata|スイーツ|チョコ|バニラ|キャラメル/i],
  ['citrus', /lemon|\blime|orange|grapefruit|citrus|yuzu|mandarin|tangerine|bergam|clementine|kumquat|pomelo|レモン|オレンジ|ライム|柚子|ゆず/i],
  ['berry', /berry|berries|cherry|currant|cassis|acai|malina|yagoda|ベリー|チェリー|苺|いちご/i],
  ['tropical', /mango|pineapple|painapple|passion|guava|papaya|coconut|banana|lychee|litchi|kiwi|tropic|dragon|maracuja|feijoa|ananas|coco|マンゴー|パイン|ココナッツ|バナナ/i],
  ['orchard', /apple|peach|\bpears?\b|grape|plum|apricot|melon|\bfigs?\b|pomegranate|quince|nectarine|persimmon|アップル|ピーチ|グレープ|メロン|桃|梨/i],
  ['botanical', /rose|jasmine|lavender|flower|floral|\btea\b|\bchai\b|cola|wine|cocktail|mojito|colada|spice|cardamom|anise|clove|ginger|basil|tarragon|herb|tobacco|whisk|\brum\b|\bgin\b|beer|soda|lemonade|energy|masala|elder|linden|osmanthus|sakura|matcha|cucumber|cardamon|\bearl\b|kola|guinness|cedar|wood|cactus|ローズ|ティー|コーラ|スパイス/i],
];

export function familyOf(name: string): Family {
  for (const [family, re] of RULES) {
    if (re.test(name)) return family;
  }
  return 'other';
}
