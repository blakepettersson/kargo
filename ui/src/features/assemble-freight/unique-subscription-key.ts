import { Chart, GitCommit, Image } from '@ui/gen/api/v1alpha1/generated_pb';

import { DiscoveryResult } from './types';

export const getSubscriptionKey = (res: DiscoveryResult) => {
  if (res.$typeName === 'github.com.akuity.kargo.api.v1alpha1.DiscoveryResult') {
    return res.name;
  }

  if (res.$typeName === 'github.com.akuity.kargo.api.v1alpha1.ChartDiscoveryResult') {
    const base = `${res.repoURL}/${res.name}`;
    return res.alias ? `${base}:${res.alias}` : base;
  }

  if ('alias' in res && res.alias) {
    return `${res.repoURL}:${res.alias}`;
  }

  return res.repoURL;
};

export const getSubscriptionKeyFreight = (res: Image | Chart | GitCommit) => {
  if (res.$typeName === 'github.com.akuity.kargo.api.v1alpha1.Chart') {
    const base = `${res.repoURL}/${res.name}`;
    return res.alias ? `${base}:${res.alias}` : base;
  }

  if ('alias' in res && res.alias) {
    return `${res.repoURL}:${res.alias}`;
  }

  return res.repoURL;
};

export const isEqualSubscriptions = (a: DiscoveryResult, b: DiscoveryResult) =>
  getSubscriptionKey(a) === getSubscriptionKey(b);
