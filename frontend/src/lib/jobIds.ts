export function uniqueJobIds(jobIds: string[]): string[] {
  if (jobIds.length < 2) {
    return jobIds;
  }

  return [...new Set(jobIds)];
}
