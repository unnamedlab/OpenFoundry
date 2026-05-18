import { describe, expect, it } from 'vitest';

import {
  folderStablePath,
  projectStablePath,
  resourceIDFromStableSegment,
  resourceLocatorFromStableSegment,
  stableRIDSegment,
  workspaceResourceStablePath,
} from './stableResourceUrls';

const projectID = '018f2f1c-aaaa-7bbb-8ccc-000000000001';
const folderID = '018f2f1c-aaaa-7bbb-8ccc-000000000002';

describe('stable resource URLs', () => {
  it('keeps the RID as the route identity and treats slugs as visual sugar', () => {
    const segment = stableRIDSegment(`ri.compass.main.project.${projectID}`, 'Ops Finance');

    expect(segment).toBe(`ri.compass.main.project.${projectID}--ops-finance`);
    expect(resourceIDFromStableSegment(segment)).toBe(`ri.compass.main.project.${projectID}`);
    expect(resourceLocatorFromStableSegment(segment)).toBe(projectID);
  });

  it('builds Compass project and folder URLs from RIDs', () => {
    const project = { id: projectID, slug: 'finance', display_name: 'Finance Ops' };
    const folder = { id: folderID, name: 'Published datasets' };

    expect(projectStablePath(project)).toBe(`/projects/ri.compass.main.project.${projectID}--finance-ops`);
    expect(folderStablePath(project, folder)).toBe(
      `/projects/ri.compass.main.project.${projectID}--finance-ops/folders/ri.compass.main.folder.${folderID}--published-datasets`,
    );
  });

  it('builds app resource URLs with RIDs instead of local UUID-only paths', () => {
    expect(workspaceResourceStablePath('dataset', projectID)).toBe(
      `/datasets/ri.foundry.main.dataset.${projectID}`,
    );
  });
});
