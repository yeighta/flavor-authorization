// Mirror of internal/model/types.go shapes serialized into data/products.json.

export type SourceKind = 'shinki' | 'henkou' | 'unknown';

export interface PricePoint {
  date: string;
  priceYen: number;
  source: SourceKind;
  sourceUrl: string;
}

export interface Product {
  category: string;
  manufacturer: string;
  name: string;
  variant?: string;
  grams: string;
  priceYen: number;
  country?: string;
  updatedDate: string;
  source: SourceKind;
  sourceUrl: string;
  history?: PricePoint[];
}
