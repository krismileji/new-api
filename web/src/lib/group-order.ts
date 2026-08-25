export function orderGroupNames(
  groups: string[],
  configuredOrder: readonly string[] | null | undefined = []
): string[] {
  const safeConfiguredOrder = Array.isArray(configuredOrder)
    ? configuredOrder
    : []
  const rank = new Map(
    safeConfiguredOrder.map((group, index) => [group, index])
  )
  return [...groups].sort((left, right) => {
    const leftRank = rank.get(left)
    const rightRank = rank.get(right)
    if (leftRank !== undefined && rightRank !== undefined) {
      return leftRank - rightRank
    }
    if (leftRank !== undefined) return -1
    if (rightRank !== undefined) return 1
    return left.localeCompare(right)
  })
}
