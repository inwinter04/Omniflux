export interface VariableDraft {
  name: string;
  type: 'string' | 'number' | 'integer' | 'boolean';
  description: string;
  required: boolean;
  default: string | number | boolean | null;
}
