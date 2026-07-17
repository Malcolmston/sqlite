import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { NodeVsGo } from '../../../src/components/NodeVsGo';
import { SQLITE } from '../../../src/data';

describe('NodeVsGo', () => {
  it('renders the comparison heading and both C and Go columns', () => {
    const { container } = render(<NodeVsGo lib={SQLITE} />);
    expect(container.querySelector(`#${SQLITE.id}-cmp`)).not.toBeNull();
    expect(screen.getByText('C')).toBeInTheDocument();
    expect(screen.getByText('Go')).toBeInTheDocument();
    expect(container.querySelectorAll('.compare .code').length).toBe(2);
  });
});
