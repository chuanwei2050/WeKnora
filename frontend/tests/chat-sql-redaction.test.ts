import { describe, expect, it } from 'vitest';

import { redactSqlFromAnswer } from '../src/utils/chatMessageShared';

describe('redactSqlFromAnswer', () => {
  it('replaces inline SELECT statements without changing the answer', () => {
    const answer = "共有 41 人。依据：`SELECT COUNT(*) FROM data WHERE 学历 = '硕士'`，结果为 41。";

    expect(redactSqlFromAnswer(answer)).toBe('共有 41 人。依据：`结构化查询`，结果为 41。');
  });

  it('replaces fenced SQL and leaves ordinary code untouched', () => {
    const answer = '查询结果如下：\n```sql\nSELECT * FROM data\n```\n字段为 `master_count`。';

    expect(redactSqlFromAnswer(answer)).toBe('查询结果如下：\n`结构化查询`\n字段为 `master_count`。');
  });
});
