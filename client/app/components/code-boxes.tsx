import { useEffect, useRef } from 'react';
import type { ClipboardEvent, KeyboardEvent } from 'react';

import {
  applyCodeInput,
  normalizeCodeInput,
  type CodeInputKind,
} from '../lib/code-input';

interface CodeBoxesProps {
  ariaLabel: string;
  characters: readonly string[];
  disabled?: boolean;
  kind: CodeInputKind;
  length: number;
  onChange: (characters: string[]) => void;
}

export function CodeBoxes({
  ariaLabel,
  characters,
  disabled = false,
  kind,
  length,
  onChange,
}: CodeBoxesProps) {
  const inputs = useRef<Array<HTMLInputElement | null>>([]);
  const previousCharacters = useRef(characters);

  useEffect(() => {
    if (previousCharacters.current.some(Boolean) && characters.every((character) => !character)) {
      inputs.current[0]?.focus();
    }
    previousCharacters.current = characters;
  }, [characters]);

  function updateCode(value: string, focusIndex?: number) {
    const normalized = normalizeCodeInput(value, length, kind);
    onChange(applyCodeInput(characters, normalized, focusIndex, length));
    if (focusIndex !== undefined && normalized.length === 1 && focusIndex < length - 1) {
      inputs.current[focusIndex + 1]?.focus();
    }
  }

  function handlePaste(event: ClipboardEvent<HTMLInputElement>) {
    event.preventDefault();
    const pastedCode = normalizeCodeInput(event.clipboardData.getData('text'), length, kind);
    updateCode(pastedCode);
    inputs.current[Math.min(pastedCode.length, length - 1)]?.focus();
  }

  function handleKeyDown(event: KeyboardEvent<HTMLInputElement>, index: number) {
    if (event.key === 'Backspace' && characters[index] === '' && index > 0) {
      inputs.current[index - 1]?.focus();
    }
    if (event.key === 'ArrowLeft' && index > 0) inputs.current[index - 1]?.focus();
    if (event.key === 'ArrowRight' && index < length - 1) inputs.current[index + 1]?.focus();
  }

  return (
    <div className="flex justify-center gap-1 sm:gap-2" role="group" aria-label={ariaLabel}>
      {Array.from({ length }, (_, index) => (
        <input
          key={index}
          ref={(element) => {
            inputs.current[index] = element;
          }}
          aria-label={`${ariaLabel} character ${index + 1}`}
          autoComplete={index === 0 ? 'one-time-code' : 'off'}
          className="size-9 rounded-lg border border-line bg-field text-center font-mono text-lg text-fg outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/25 sm:size-10"
          inputMode={kind === 'numeric' ? 'numeric' : 'text'}
          maxLength={1}
          type="text"
          value={characters[index] ?? ''}
          disabled={disabled}
          onChange={(event) => updateCode(event.target.value, index)}
          onKeyDown={(event) => handleKeyDown(event, index)}
          onPaste={handlePaste}
        />
      ))}
    </div>
  );
}
