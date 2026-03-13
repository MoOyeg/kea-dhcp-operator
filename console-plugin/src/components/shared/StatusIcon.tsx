import React from 'react';
import {
  CheckCircleIcon,
  ExclamationCircleIcon,
  InProgressIcon,
  QuestionCircleIcon,
} from '@patternfly/react-icons';

interface StatusIconProps {
  phase?: string;
}

/**
 * Renders a PatternFly icon based on the component phase string.
 *  - "Running"      -> green check
 *  - "Progressing"  -> blue in-progress
 *  - "Error"        -> red exclamation
 *  - default        -> grey question mark
 */
const StatusIcon: React.FC<StatusIconProps> = ({ phase }) => {
  switch (phase) {
    case 'Running':
      return <CheckCircleIcon color="var(--pf-v5-global--success-color--100, #3e8635)" />;
    case 'Progressing':
      return <InProgressIcon color="var(--pf-v5-global--info-color--100, #2b9af3)" />;
    case 'Error':
      return <ExclamationCircleIcon color="var(--pf-v5-global--danger-color--100, #c9190b)" />;
    default:
      return <QuestionCircleIcon color="var(--pf-v5-global--disabled-color--100, #6a6e73)" />;
  }
};

export default StatusIcon;
