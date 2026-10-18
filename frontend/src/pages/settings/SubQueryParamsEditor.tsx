import { useEffect, useRef, useState } from 'react';
import { AutoComplete, Button, Input, Typography } from 'antd';
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons';
import { useTranslation } from 'react-i18next';
import {
  SUB_QUERY_KEY_PRESETS,
  SUB_QUERY_MAX_PARAMS,
  SUB_QUERY_MAX_VALUE_LEN,
  appendSubQuery,
  isValidSubQueryKey,
  parseSubQueryParams,
  serializeSubQueryParams,
  type SubQueryParam,
} from '@/lib/sub-query';

interface SubQueryParamsEditorProps {
  value: string;
  onChange: (next: string) => void;
}

// Rows are kept locally so a half-typed key is not dropped while editing; only
// valid rows are written to the setting.
export default function SubQueryParamsEditor({ value, onChange }: SubQueryParamsEditorProps) {
  const { t } = useTranslation();
  const [rows, setRows] = useState<SubQueryParam[]>(() => parseSubQueryParams(value));
  const lastEmitted = useRef(value);

  useEffect(() => {
    if (value !== lastEmitted.current) {
      lastEmitted.current = value;
      setRows(parseSubQueryParams(value));
    }
  }, [value]);

  function commit(next: SubQueryParam[]) {
    setRows(next);
    const valid = next.filter((r) => isValidSubQueryKey(r.key));
    const serialized = serializeSubQueryParams(valid);
    lastEmitted.current = serialized;
    onChange(serialized);
  }

  const used = new Set(rows.map((r) => r.key.trim()));
  const options = SUB_QUERY_KEY_PRESETS.filter((k) => !used.has(k)).map((k) => ({ value: k }));
  const preview = appendSubQuery(
    'https://example.com/sub/SUBID',
    serializeSubQueryParams(rows.filter((r) => isValidSubQueryKey(r.key))),
  );

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
      {rows.map((row, index) => {
        const keyInvalid = row.key.trim() !== '' && !isValidSubQueryKey(row.key);
        return (
          <div key={index} style={{ display: 'flex', gap: 8 }}>
            <AutoComplete
              value={row.key}
              options={options}
              placeholder="key"
              status={keyInvalid ? 'error' : undefined}
              style={{ width: 180 }}
              onChange={(key) => commit(rows.map((r, i) => (i === index ? { ...r, key } : r)))}
            />
            <Input
              value={row.value}
              placeholder="value"
              maxLength={SUB_QUERY_MAX_VALUE_LEN}
              onChange={(e) =>
                commit(rows.map((r, i) => (i === index ? { ...r, value: e.target.value } : r)))
              }
            />
            <Button
              aria-label="remove"
              icon={<DeleteOutlined />}
              onClick={() => commit(rows.filter((_, i) => i !== index))}
            />
          </div>
        );
      })}
      <div>
        <Button
          icon={<PlusOutlined />}
          disabled={rows.length >= SUB_QUERY_MAX_PARAMS}
          onClick={() => setRows([...rows, { key: '', value: '' }])}
        >
          {t('pages.settings.subQueryParamsAdd')}
        </Button>
      </div>
      {preview !== 'https://example.com/sub/SUBID' && (
        <Typography.Text type="secondary" code copyable style={{ wordBreak: 'break-all' }}>
          {preview}
        </Typography.Text>
      )}
    </div>
  );
}
