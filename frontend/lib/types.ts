// Mirror of internal/model/types.go shapes serialized into data/products.json + data/manufacturers.json.

export type Category =
  | 'パイプたばこ'
  | '紙巻たばこ'
  | '葉巻たばこ'
  | '刻みたばこ'
  | 'その他';

export type PipeType = 'kiseru' | 'shisha' | 'unknown';
export type Confidence = 'high' | 'medium' | 'low';
export type SourceKind = 'shinki' | 'henkou' | 'unknown';

export interface Product {
  category: Category;
  manufacturer: string;
  name: string;
  variant?: string;
  grams: string;
  priceYen: number;
  country?: string;
  pipeType?: PipeType;
  updatedDate: string;
  source: SourceKind;
  sourceUrl: string;
}

export interface Classification {
  manufacturer: string;
  type: PipeType;
  confidence: Confidence;
  reason?: string;
}

export interface ManufacturerMap {
  entries: Record<string, Classification>;
}

export interface PipeProduct extends Product {
  pipeType: PipeType;
  confidence?: Confidence;
}
